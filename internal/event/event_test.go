package event

import "testing"

func TestCanonicalDigest_StableAcrossKeyOrderAndWhitespace(t *testing.T) {
	a := []byte(`{"b":2,"a":1}`)
	b := []byte(`{ "a" : 1, "b" : 2 }`)
	da, err := CanonicalDigest(a)
	if err != nil {
		t.Fatal(err)
	}
	db, err := CanonicalDigest(b)
	if err != nil {
		t.Fatal(err)
	}
	if da != db {
		t.Fatalf("键序与空白不同但内容相同，摘要必须一致：%s != %s", da, db)
	}
	if len(da) != 7+64 { // sha256: + 64 hex
		t.Fatalf("摘要形状错误: %s", da)
	}
}

func TestCanonicalDigest_DistinguishesValues(t *testing.T) {
	d1, _ := CanonicalDigest([]byte(`{"a":1}`))
	d2, _ := CanonicalDigest([]byte(`{"a":2}`))
	if d1 == d2 {
		t.Fatal("不同值不应产生相同摘要")
	}
}

func TestEnvelope_RejectsBadShape(t *testing.T) {
	env := Envelope{
		SchemaVersion: "1", EventID: "EVT-000001", Source: "SRC-1",
		EventType: "entity.registered", SubjectRef: "CITY-1",
		OccurredAt: "2026-01-01T00:00:00+08:00", SourceSequence: 1,
		PayloadDigest: "sha256:bad",
		Payload:       jsonRaw(`{}`),
	}
	// 错误摘要形状。
	if _, err := env.Validate(); err == nil {
		t.Fatal("非法摘要形状应被拒绝")
	}

	// 缺少时区偏移的时间。
	env.PayloadDigest = mustDigest(t, `{}`)
	env.OccurredAt = "2026-01-01T00:00:00"
	if _, err := env.Validate(); err == nil {
		t.Fatal("无偏移时间应被拒绝")
	}

	// 摘要与内容不符。
	env.OccurredAt = "2026-01-01T00:00:00+08:00"
	env.PayloadDigest = mustDigest(t, `{"x":1}`)
	env.Payload = jsonRaw(`{"x":2}`)
	if _, err := env.Validate(); err == nil {
		t.Fatal("摘要不匹配应被拒绝")
	}
}
