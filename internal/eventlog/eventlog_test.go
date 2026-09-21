package eventlog

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/events"
)

func sampleEnvelope(id, source string, seq int64) events.Envelope {
	p := events.EntityRegistered{
		Ref:   "ENT-TEST" + strings.Repeat("0", 2) + id[len(id)-1:],
		Level: domain.LevelCity,
		Names: domain.LocalizedText{"zh": "测试城"},
	}
	raw, _ := events.EncodePayload(p)
	return events.Envelope{
		SchemaVersion:  domain.SchemaVersion,
		EventID:        id,
		Source:         source,
		SourceSequence: seq,
		SubjectRef:     p.Ref,
		OccurredAt:     "2026-09-19T09:30:00+08:00",
		PayloadDigest:  Digest(raw),
		Payload:        raw,
	}
}

func openTempLog(t *testing.T) *Log {
	t.Helper()
	lg, err := Open(filepath.Join(t.TempDir(), "events.jsonl"))
	if err != nil {
		t.Fatalf("打开日志失败: %v", err)
	}
	return lg
}

func TestAppendSequenceAndReplay(t *testing.T) {
	lg := openTempLog(t)
	env1 := sampleEnvelope("EVT-0001", "src-a", 1)
	env2 := sampleEnvelope("EVT-0002", "src-a", 2)
	env3 := sampleEnvelope("EVT-0003", "src-b", 1) // 不同来源序号各自从 1 开始

	for _, env := range []events.Envelope{env1, env2, env3} {
		if dup, _, err := lg.Append(env); err != nil || dup {
			t.Fatalf("追加事件失败: dup=%v err=%v", dup, err)
		}
	}
	if got := lg.LastSequence("src-a"); got != 2 {
		t.Fatalf("src-a 水位应为 2，实得 %d", got)
	}

	count := 0
	if err := lg.Replay(func(events.Envelope, events.Payload) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("重放失败: %v", err)
	}
	if count != 3 {
		t.Fatalf("应重放 3 条事件，实得 %d", count)
	}
}

func TestAppendAutoSequence(t *testing.T) {
	lg := openTempLog(t)
	env := sampleEnvelope("EVT-0010", "src-a", 0)
	dup, assigned, err := lg.Append(env)
	if err != nil || dup || assigned != 1 {
		t.Fatalf("自动序号首次应为 1: dup=%v assigned=%d err=%v", dup, assigned, err)
	}
	env.EventID = "EVT-0011"
	env.Payload = tamperedPayload(t, env.Payload, "测试城二")
	env.PayloadDigest = Digest(env.Payload)
	dup, assigned, err = lg.Append(env)
	if err != nil || dup || assigned != 2 {
		t.Fatalf("自动序号第二次应为 2: dup=%v assigned=%d err=%v", dup, assigned, err)
	}
}

func TestAppendIdempotentRetry(t *testing.T) {
	lg := openTempLog(t)
	env := sampleEnvelope("EVT-0020", "src-a", 1)
	if _, _, err := lg.Append(env); err != nil {
		t.Fatalf("首次追加失败: %v", err)
	}
	dup, assigned, err := lg.Append(env) // 完全相同的重试
	if err != nil || !dup || assigned != 1 {
		t.Fatalf("相同重试应幂等: dup=%v assigned=%d err=%v", dup, assigned, err)
	}

	// 同 event_id 但内容不同 → 冲突。
	bad := env
	bad.Payload = tamperedPayload(t, env.Payload, "另一座城")
	bad.PayloadDigest = Digest(bad.Payload)
	if _, _, err := lg.Append(bad); !errors.Is(err, ErrDuplicateEvent) {
		t.Fatalf("同标识不同内容应报 ErrDuplicateEvent，实得 %v", err)
	}
}

func TestAppendRejectsSequenceGap(t *testing.T) {
	lg := openTempLog(t)
	if _, _, err := lg.Append(sampleEnvelope("EVT-0030", "src-a", 1)); err != nil {
		t.Fatalf("首次追加失败: %v", err)
	}
	if _, _, err := lg.Append(sampleEnvelope("EVT-0031", "src-a", 3)); err == nil {
		t.Fatal("序号跳跃应被拒绝")
	}
}

func TestAppendRejectsBadDigest(t *testing.T) {
	lg := openTempLog(t)
	env := sampleEnvelope("EVT-0040", "src-a", 1)
	env.PayloadDigest = "sha256:" + strings.Repeat("00", 32)
	if _, _, err := lg.Append(env); err == nil {
		t.Fatal("摘要不匹配应被拒绝")
	}
}

func TestReplayDetectsTampering(t *testing.T) {
	lg := openTempLog(t)
	env := sampleEnvelope("EVT-0050", "src-a", 1)
	if _, _, err := lg.Append(env); err != nil {
		t.Fatalf("追加失败: %v", err)
	}
	// 直接篡改日志文件内容但保留旧摘要，重放必须失败。
	path := lg.path
	content, err := readFile(path)
	if err != nil {
		t.Fatal(err)
	}
	tampered := strings.Replace(content, "测试城", "被篡改城", 1)
	if err := writeFile(path, tampered); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(path); err == nil {
		t.Fatal("篡改后的日志在打开时应无法通过摘要校验")
	}
}

func TestOffsetPreservedAndDigestFormat(t *testing.T) {
	lg := openTempLog(t)
	env := sampleEnvelope("EVT-0060", "src-a", 1)
	if _, _, err := lg.Append(env); err != nil {
		t.Fatalf("追加失败: %v", err)
	}
	if err := lg.Replay(func(e events.Envelope, _ events.Payload) error {
		if e.OccurredAt != "2026-09-19T09:30:00+08:00" {
			t.Fatalf("发生时间的偏移量必须原样保留，实得 %s", e.OccurredAt)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 摘要格式 sha256:64 位十六进制。
	sum := sha256.Sum256(env.Payload)
	if env.PayloadDigest != "sha256:"+hex.EncodeToString(sum[:]) {
		t.Fatal("摘要格式不符合契约")
	}
}

func tamperedPayload(t *testing.T, raw []byte, newName string) []byte {
	t.Helper()
	var p events.EntityRegistered
	if err := unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	p.Ref = "ENT-TEST009"
	p.Names = domain.LocalizedText{"zh": newName}
	out, err := events.EncodePayload(p)
	if err != nil {
		t.Fatal(err)
	}
	return out
}
