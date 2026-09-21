package store

import (
	"testing"

	"example.com/batch-092001-q018/internal/event"
)

func env(id, source string, seq int64, digest string) event.Envelope {
	return event.Envelope{
		SchemaVersion: "1", EventID: id, Source: source, EventType: "entity.registered",
		SubjectRef: "S", OccurredAt: "2026-01-01T00:00:00+08:00",
		SourceSequence: seq, PayloadDigest: digest,
	}
}

func TestAppend_IdempotentReplay(t *testing.T) {
	s := NewEventStore()
	e := env("EVT-000001", "SRC-A", 1, "sha256:"+digest64("a"))
	r1, err := s.Append(e, nil)
	if err != nil || !r1.Accepted {
		t.Fatalf("首次追加应受理，得到 %+v %v", r1, err)
	}
	// 同 ID 同摘要重放：幂等。
	r2, err := s.Append(e, nil)
	if err != nil || r2.Accepted {
		t.Fatalf("同内容重放应幂等，得到 %+v %v", r2, err)
	}
	if s.Len() != 1 {
		t.Fatalf("重放不得增加日志，得到 %d", s.Len())
	}
}

func TestAppend_SameIDDifferentDigestRejected(t *testing.T) {
	s := NewEventStore()
	e1 := env("EVT-000001", "SRC-A", 1, "sha256:"+digest64("a"))
	if _, err := s.Append(e1, nil); err != nil {
		t.Fatal(err)
	}
	e2 := env("EVT-000001", "SRC-A", 2, "sha256:"+digest64("b"))
	if _, err := s.Append(e2, nil); err == nil {
		t.Fatal("同 ID 不同摘要必须判为重复冲突")
	}
}

func TestAppend_SequenceMonotonicPerSource(t *testing.T) {
	s := NewEventStore()
	if _, err := s.Append(env("EVT-000001", "SRC-A", 5, "sha256:"+digest64("a")), nil); err != nil {
		t.Fatal(err)
	}
	// 允许序号缺口（直接到 10）。
	if _, err := s.Append(env("EVT-000002", "SRC-A", 10, "sha256:"+digest64("b")), nil); err != nil {
		t.Fatalf("序号缺口应允许: %v", err)
	}
	// 倒退/重复拒绝。
	if _, err := s.Append(env("EVT-000003", "SRC-A", 10, "sha256:"+digest64("c")), nil); err == nil {
		t.Fatal("同来源重复序号必须拒绝")
	}
	if _, err := s.Append(env("EVT-000004", "SRC-A", 9, "sha256:"+digest64("d")), nil); err == nil {
		t.Fatal("同来源序号倒退必须拒绝")
	}
	// 不同来源的序号独立。
	if _, err := s.Append(env("EVT-000005", "SRC-B", 1, "sha256:"+digest64("e")), nil); err != nil {
		t.Fatalf("不同来源序号应独立计数: %v", err)
	}
}

func TestAbortLast_RemovesAndRecomputesSequence(t *testing.T) {
	s := NewEventStore()
	_, _ = s.Append(env("EVT-000001", "SRC-A", 1, "sha256:"+digest64("a")), nil)
	_, _ = s.Append(env("EVT-000002", "SRC-A", 2, "sha256:"+digest64("b")), nil)
	s.AbortLast("EVT-000002")
	if s.Len() != 1 {
		t.Fatalf("回滚后应剩 1 条，得到 %d", s.Len())
	}
	// 回滚后同序号可再次写入。
	if _, err := s.Append(env("EVT-000006", "SRC-A", 2, "sha256:"+digest64("c")), nil); err != nil {
		t.Fatalf("回滚后序号 2 应可重新写入: %v", err)
	}
}

func digest64(c string) string {
	out := ""
	for i := 0; i < 64; i++ {
		out += c
	}
	return out
}
