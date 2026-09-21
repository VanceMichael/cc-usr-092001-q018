package eventlog

import (
	"encoding/json"
	"os"
	"testing"

	"example.com/batch-092001-q018/internal/events"
)

// TestContractExampleDigest 校验 contracts/event.example.json 中的载荷摘要
// 与服务端的确定性摘要算法一致，且载荷类型可解码。
func TestContractExampleDigest(t *testing.T) {
	raw, err := os.ReadFile("../../contracts/event.example.json")
	if err != nil {
		t.Fatal(err)
	}
	var env events.Envelope
	if err := json.Unmarshal(raw, &env); err != nil {
		t.Fatal(err)
	}
	if got := Digest(env.Payload); got != env.PayloadDigest {
		t.Fatalf("契约示例摘要与实现不一致: 文件 %s, 实得 %s", env.PayloadDigest, got)
	}
	if _, p, err := events.DecodePayload(env.Payload); err != nil {
		t.Fatalf("契约示例载荷无法解码: %v", err)
	} else if p.PayloadType() != events.TypeRelationConcluded {
		t.Fatalf("契约示例载荷类型不符: %s", p.PayloadType())
	}
}
