package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"testing"
)

func digestFor(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// TestIngestEnvelopeEndpoint 验证外部组装信封的摄入：
// 调用方自带正确摘要，服务端验摘要、按来源序号落盘；重试幂等。
func TestIngestEnvelopeEndpoint(t *testing.T) {
	h := newTestServer(t)

	payload := map[string]any{
		"type":  "entity_registered",
		"ref":   "ENT-ENV1",
		"level": "city",
		"names": map[string]string{"zh": "信封城"},
	}
	raw, _ := json.Marshal(payload)
	env := map[string]any{
		"schema_version":  "1",
		"event_id":        "EVT-EXT-001",
		"source":          "partner-office-b",
		"source_sequence": 1,
		"subject_ref":     "ENT-ENV1",
		"occurred_at":     "2026-08-01T08:00:00+02:00",
		"payload_digest":  digestFor(raw),
		"payload":         payload,
	}
	rec, _ := doJSON(t, h, http.MethodPost, "/v1/events", env)
	if rec.Code != http.StatusCreated {
		t.Fatalf("外部信封首次摄入应 201，实得 %d %s", rec.Code, rec.Body.String())
	}
	// 完全相同的信封重试 → 幂等。
	rec, _ = doJSON(t, h, http.MethodPost, "/v1/events", env)
	if rec.Code != http.StatusOK {
		t.Fatalf("相同信封重试应幂等 200，实得 %d", rec.Code)
	}
	// 摘要错误必须拒绝。
	env["event_id"] = "EVT-EXT-002"
	env["payload_digest"] = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	rec, _ = doJSON(t, h, http.MethodPost, "/v1/events", env)
	if rec.Code != http.StatusConflict {
		t.Fatalf("摘要不匹配应 409，实得 %d", rec.Code)
	}
	// 投影生效：实体可查。
	rec, _ = doJSON(t, h, http.MethodGet, "/v1/entities/ENT-ENV1", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("信封摄入的实体应可查，实得 %d", rec.Code)
	}
}
