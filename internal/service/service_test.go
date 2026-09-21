package service

import (
	"path/filepath"
	"testing"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/events"
	"example.com/batch-092001-q018/internal/store"
)

func openTempService(t *testing.T) (*Service, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	st, err := store.Open(path)
	if err != nil {
		t.Fatalf("打开存储失败: %v", err)
	}
	return New(st, "test-source"), path
}

func TestCommitAutoSequenceAndIdempotency(t *testing.T) {
	svc, _ := openTempService(t)
	p := events.EntityRegistered{Ref: "ENT-AAA", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "甲城"}}

	// 首次：自动分配事件标识与序号 1。
	res, err := svc.Commit(CommitMeta{OccurredAt: "2026-09-01T10:00:00+08:00"}, p)
	if err != nil {
		t.Fatalf("首次提交失败: %v", err)
	}
	if res.SourceSequence != 1 || res.Duplicated || res.EventID == "" {
		t.Fatalf("首次提交结果不符: %+v", res)
	}
	// 同 event_id 重试：幂等返回，序号保持 1。
	res2, err := svc.Commit(CommitMeta{
		EventID: res.EventID, OccurredAt: "2026-09-01T10:00:00+08:00",
	}, p)
	if err != nil {
		t.Fatalf("幂等重试不应报错: %v", err)
	}
	if !res2.Duplicated || res2.SourceSequence != 1 {
		t.Fatalf("重试应幂等且序号为 1: %+v", res2)
	}
	// 下一条自动序号应为 2。
	p2 := events.EntityRegistered{Ref: "ENT-BBB", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "乙城"}}
	res3, err := svc.Commit(CommitMeta{OccurredAt: "2026-09-01T11:00:00+08:00"}, p2)
	if err != nil {
		t.Fatalf("第二次提交失败: %v", err)
	}
	if res3.SourceSequence != 2 {
		t.Fatalf("自动序号应为 2，实得 %d", res3.SourceSequence)
	}
}

func TestReopenRestoresProjection(t *testing.T) {
	svc, path := openTempService(t)
	_, err := svc.Commit(CommitMeta{OccurredAt: "2026-09-01T10:00:00+08:00"},
		events.EntityRegistered{Ref: "ENT-R01", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "重放城"}})
	if err != nil {
		t.Fatal(err)
	}

	// 重新打开同一日志：投影必须由事件重建。
	st2, err := store.Open(path)
	if err != nil {
		t.Fatalf("重新打开失败: %v", err)
	}
	svc2 := New(st2, "test-source")
	d, err := svc2.EntityDossier("ENT-R01")
	if err != nil {
		t.Fatalf("重放后实体应可查: %v", err)
	}
	if d.CurrentRef != "ENT-R01" || len(d.Entity.Names) != 1 {
		t.Fatalf("重放投影不符: %+v", d)
	}
	// 来源序号水位也恢复：下一条自动序号为 2。
	res, err := svc2.Commit(CommitMeta{OccurredAt: "2026-09-01T12:00:00+08:00"},
		events.EntityRegistered{Ref: "ENT-R02", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "续上城"}})
	if err != nil {
		t.Fatalf("重开后提交失败: %v", err)
	}
	if res.SourceSequence != 2 {
		t.Fatalf("重开后序号水位应恢复，期望 2 实得 %d", res.SourceSequence)
	}
}

func TestValidationAndConflictErrors(t *testing.T) {
	svc, _ := openTempService(t)
	// 形状错误 → ValidationError。
	if _, err := svc.Commit(CommitMeta{OccurredAt: "2026-09-01T10:00:00+08:00"},
		events.EntityRegistered{Ref: "BADREF", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "x"}}); err == nil {
		t.Fatal("非法引用应报错")
	}
	// 查询不存在实体 → NotFoundError。
	if _, err := svc.EntityDossier("ENT-NOPE00"); err == nil {
		t.Fatal("不存在实体应报错")
	}
}
