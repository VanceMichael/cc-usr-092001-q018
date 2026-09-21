package service_test

import (
	"testing"
	"time"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/service"
	"example.com/batch-092001-q018/internal/testsupport"
)

// registerCity 登记一个城市并返回其引用。
func registerCity(t *testing.T, svc *service.Service, ref, name string) {
	t.Helper()
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(),
		event.TypeEntityRegistered, ref, "2026-01-01T00:00:00+08:00",
		event.RegisterEntityPayload{Ref: ref, Level: domain.LevelCity, CurrentName: name, Language: "zh"}))
}

func TestRegisterEntity_RejectsBadLevelAndDuplicate(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()

	bad := testsupport.Envelope(t, testsupport.EventID(), event.TypeEntityRegistered, "CITY-0001",
		"2026-01-01T00:00:00+08:00", event.RegisterEntityPayload{
			Ref: "CITY-0001", Level: domain.Level("continent"), CurrentName: "X",
		})
	testsupport.FailIngest(t, svc, bad, domain.CodeValidation)

	registerCity(t, svc, "CITY-0001", "临江市")
	dup := testsupport.Envelope(t, testsupport.EventID(), event.TypeEntityRegistered, "CITY-0001",
		"2026-01-02T00:00:00+08:00", event.RegisterEntityPayload{
			Ref: "CITY-0001", Level: domain.LevelCity, CurrentName: "临江市",
		})
	testsupport.FailIngest(t, svc, dup, domain.CodeReference)
}

func TestRename_KeepsHistoricalNameTraceable(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()
	registerCity(t, svc, "CITY-0001", "旧州市")

	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeEntityRenamed, "CITY-0001",
		"2026-03-01T00:00:00+08:00", event.RenameEntityPayload{
			Ref: "CITY-0001", NewName: "新洲市", Language: "zh",
		}))

	got, err := svc.GetEntity("CITY-0001")
	if err != nil {
		t.Fatal(err)
	}
	if got.CurrentName != "新洲市" {
		t.Fatalf("当前名称应为新洲市，得到 %q", got.CurrentName)
	}
	if !got.HasName("旧州市") {
		t.Fatal("更名后旧名称必须保留可追溯")
	}
	res := svc.ResolveName("旧州市")
	if len(res.Matches) != 1 || res.Matches[0].MatchedOn != "historical" {
		t.Fatalf("应能按历史名解析到实体，得到 %+v", res.Matches)
	}
}

func TestSameCityName_IsAmbiguous(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()
	registerCity(t, svc, "CITY-0001", "斯普林菲尔德")
	registerCity(t, svc, "CITY-0002", "斯普林菲尔德")

	res := svc.ResolveName("斯普林菲尔德")
	if !res.Ambiguous || len(res.Matches) != 2 {
		t.Fatalf("同名不同实体应标记歧义，得到 ambiguous=%v matches=%d", res.Ambiguous, len(res.Matches))
	}
}

func TestMerge_PreservesOldRelationsAndResolvesByName(t *testing.T) {
	testsupport.ResetSeq()
	svc := service.New()
	registerCity(t, svc, "CITY-0001", "甲城市")
	registerCity(t, svc, "CITY-0002", "乙城市")
	registerCity(t, svc, "CITY-9000", "承接市")

	// 甲城市 与 乙城市 先有意向关系。
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationProposed, "REL-0001",
		"2026-02-01T00:00:00+08:00", event.ProposeRelationPayload{
			Ref: "REL-0001", PartyA: "CITY-0001", PartyB: "CITY-0002",
		}))

	// 甲城市 并入 承接市。
	testsupport.MustIngest(t, svc, testsupport.Envelope(t, testsupport.EventID(), event.TypeEntityMerged, "CITY-9000",
		"2026-04-01T00:00:00+08:00", event.MergeEntitiesPayload{
			SourceRefs: []string{"CITY-0001"}, TargetRef: "CITY-9000",
		}))

	// 旧关系仍可查询。
	view, err := svc.GetRelationPair("REL-0001", time.Time{})
	if err != nil {
		t.Fatalf("合并后旧关系必须仍可追溯: %v", err)
	}
	if view.Relation.PartyA != "CITY-0001" {
		t.Fatalf("旧关系一方引用应保留为旧主体，得到 %q", view.Relation.PartyA)
	}

	// 按旧名仍可检索：旧主体（已合并）与承接实体（带合并别名）都命中，且都解析到承接实体。
	res := svc.ResolveName("甲城市")
	if len(res.Matches) == 0 {
		t.Fatal("旧名应仍可检索")
	}
	for _, m := range res.Matches {
		if m.CurrentRef != "CITY-9000" {
			t.Fatalf("旧名命中项应解析到承接实体 CITY-9000，%s 得到 %q", m.Ref, m.CurrentRef)
		}
	}

	// 不能再对已合并的旧主体提出新关系。
	bad := testsupport.Envelope(t, testsupport.EventID(), event.TypeRelationProposed, "REL-0002",
		"2026-05-01T00:00:00+08:00", event.ProposeRelationPayload{
			Ref: "REL-0002", PartyA: "CITY-0001", PartyB: "CITY-0002",
		})
	testsupport.FailIngest(t, svc, bad, domain.CodePartyMerged)
}
