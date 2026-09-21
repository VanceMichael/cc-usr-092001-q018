package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/service"
)

// postEvent 计算摘要并投递一个完整事件信封，返回响应记录器。
func postEvent(t *testing.T, h http.Handler, env event.Envelope, payload any) *httptest.ResponseRecorder {
	t.Helper()
	raw, _ := json.Marshal(payload)
	env.Payload = raw
	d, err := event.CanonicalDigest(raw)
	if err != nil {
		t.Fatal(err)
	}
	env.PayloadDigest = d
	body, _ := json.Marshal(env)
	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

func baseEnvelope(id string, seq int64, eventType, subject string) event.Envelope {
	return event.Envelope{
		SchemaVersion: "1", EventID: id, Source: "SRC-HTTP-1",
		EventType: eventType, SubjectRef: subject,
		OccurredAt: "2026-01-01T00:00:00+08:00", SourceSequence: seq,
	}
}

func TestHealth(t *testing.T) {
	h := New(service.New())
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "ok") {
		t.Fatalf("健康检查异常: %d %s", w.Code, w.Body.String())
	}
}

func TestIngestEvent_CreateThenIdempotentReplay(t *testing.T) {
	h := New(service.New())
	payload := event.RegisterEntityPayload{Ref: "CITY-0001", Level: domain.LevelCity, CurrentName: "临江市", Language: "zh"}

	w1 := postEvent(t, h, baseEnvelope("EVT-000001", 1, event.TypeEntityRegistered, "CITY-0001"), payload)
	if w1.Code != http.StatusCreated {
		t.Fatalf("首次受理应为 201，得到 %d: %s", w1.Code, w1.Body.String())
	}
	// 同一信封重放：幂等 200。
	w2 := postEvent(t, h, baseEnvelope("EVT-000001", 1, event.TypeEntityRegistered, "CITY-0001"), payload)
	if w2.Code != http.StatusOK {
		t.Fatalf("幂等重放应为 200，得到 %d: %s", w2.Code, w2.Body.String())
	}
	var res map[string]any
	_ = json.Unmarshal(w2.Body.Bytes(), &res)
	if res["idempotent"] != true {
		t.Fatalf("重放应标记 idempotent=true，得到 %v", res["idempotent"])
	}
}

func TestIngestEvent_SequenceConflict(t *testing.T) {
	h := New(service.New())
	p1 := event.RegisterEntityPayload{Ref: "CITY-0001", Level: domain.LevelCity, CurrentName: "甲"}
	p2 := event.RegisterEntityPayload{Ref: "CITY-0002", Level: domain.LevelCity, CurrentName: "乙"}
	postEvent(t, h, baseEnvelope("EVT-000001", 5, event.TypeEntityRegistered, "CITY-0001"), p1)
	w := postEvent(t, h, baseEnvelope("EVT-000002", 5, event.TypeEntityRegistered, "CITY-0002"), p2)
	if w.Code != http.StatusConflict {
		t.Fatalf("序号冲突应为 409，得到 %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), string(domain.CodeSequenceConflict)) {
		t.Fatalf("响应应含错误码，得到 %s", w.Body.String())
	}
}

func TestIngestEvent_BadDigest(t *testing.T) {
	h := New(service.New())
	env := baseEnvelope("EVT-000001", 1, event.TypeEntityRegistered, "CITY-0001")
	env.Payload = json.RawMessage(`{"ref":"CITY-0001","level":"city","current_name":"X"}`)
	env.PayloadDigest = "sha265:" + strings.Repeat("0", 64) // 拼写错误的算法
	body, _ := json.Marshal(env)
	req := httptest.NewRequest(http.MethodPost, "/v1/events", bytes.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非法摘要应为 400，得到 %d", w.Code)
	}
}

func TestRelationEndpoint_NotFound(t *testing.T) {
	h := New(service.New())
	req := httptest.NewRequest(http.MethodGet, "/v1/relations/REL-4040", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("未知关系应为 404，得到 %d", w.Code)
	}
}

func TestNameResolveEndpoint(t *testing.T) {
	h := New(service.New())
	payload := event.RegisterEntityPayload{Ref: "CITY-0001", Level: domain.LevelCity, CurrentName: "斯市", Language: "zh"}
	postEvent(t, h, baseEnvelope("EVT-000001", 1, event.TypeEntityRegistered, "CITY-0001"), payload)

	req := httptest.NewRequest(http.MethodGet, "/v1/names/resolve?name="+"斯市", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("名称解析应为 200，得到 %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "CITY-0001") {
		t.Fatalf("应解析到 CITY-0001：%s", w.Body.String())
	}
}

func TestAnalyzeEndpoint(t *testing.T) {
	h := New(service.New())
	payload := event.RegisterEntityPayload{Ref: "CITY-0001", Level: domain.LevelCity, CurrentName: "甲市"}
	postEvent(t, h, baseEnvelope("EVT-000001", 1, event.TypeEntityRegistered, "CITY-0001"), payload)

	reqBody := `{"party_a":{"ref":"CITY-0001"},"party_b":{"name":"不存在的城"},"categories":["culture"]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/proposals:analyze", strings.NewReader(reqBody))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("分析只读应返回 200，得到 %d: %s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "create_relation") {
		t.Fatalf("未解析到对方但无歧义时应给出建议：%s", w.Body.String())
	}
}
