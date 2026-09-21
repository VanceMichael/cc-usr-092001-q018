package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/events"
	"example.com/batch-092001-q018/internal/service"
	"example.com/batch-092001-q018/internal/store"
)

func newTestServer(t *testing.T) http.Handler {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.jsonl")
	st, err := store.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	return New(service.New(st, "test-source"))
}

func doJSON(t *testing.T, h http.Handler, method, path string, body any) (*httptest.ResponseRecorder, map[string]any) {
	t.Helper()
	var raw bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&raw).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &raw)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var decoded map[string]any
	if rec.Body.Len() > 0 {
		_ = json.Unmarshal(rec.Body.Bytes(), &decoded)
	}
	return rec, decoded
}

func payloadBody(p events.Payload) map[string]any {
	b, _ := json.Marshal(p)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return map[string]any{"payload": m}
}

func TestHealthAndFullFlow(t *testing.T) {
	h := newTestServer(t)

	rec, body := doJSON(t, h, http.MethodGet, "/health", nil)
	if rec.Code != http.StatusOK || body["status"] != "ok" {
		t.Fatalf("健康检查失败: %d %v", rec.Code, body)
	}

	// 登记两座城市。
	for _, ref := range []string{"ENT-AAAA", "ENT-BBBB"} {
		rec, _ = doJSON(t, h, http.MethodPost, "/v1/entities", payloadBody(events.EntityRegistered{
			Ref: ref, Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "城市" + ref},
		}))
		if rec.Code != http.StatusCreated {
			t.Fatalf("登记实体失败: %d %s", rec.Code, rec.Body.String())
		}
	}

	// 提出意向关系。
	rec, _ = doJSON(t, h, http.MethodPost, "/v1/relations", payloadBody(events.RelationProposed{
		Ref: "REL-0001", EntityA: "ENT-AAAA", EntityB: "ENT-BBBB", ResponsibleDept: "外办",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("提出关系失败: %d %s", rec.Code, rec.Body.String())
	}
	// 重复上报同一对（反序）→ 409。
	rec, _ = doJSON(t, h, http.MethodPost, "/v1/relations", payloadBody(events.RelationProposed{
		Ref: "REL-0002", EntityA: "ENT-BBBB", EntityB: "ENT-AAAA",
	}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("重复关系应返回 409，实得 %d", rec.Code)
	}

	// 双边批准前缔结 → 409。
	rec, _ = doJSON(t, h, http.MethodPost, "/v1/relations/REL-0001/conclude", payloadBody(events.RelationConcluded{
		SigningDate: "2026-02-01", EffectiveDate: "2026-03-01",
	}))
	if rec.Code != http.StatusConflict {
		t.Fatalf("批准不齐时缔结应 409，实得 %d", rec.Code)
	}
	// 两侧批准。
	for _, ap := range []events.ApprovalRecorded{
		{Ref: "APR-0001", Side: domain.SideA, Decision: domain.ApprovalApproved, Authority: "甲市议会", DecidedAt: "2026-01-01"},
		{Ref: "APR-0002", Side: domain.SideB, Decision: domain.ApprovalApproved, Authority: "乙市议会", DecidedAt: "2026-01-02"},
	} {
		rec, _ = doJSON(t, h, http.MethodPost, "/v1/relations/REL-0001/approvals", payloadBody(ap))
		if rec.Code != http.StatusCreated {
			t.Fatalf("批准失败: %d %s", rec.Code, rec.Body.String())
		}
	}
	// 正式缔结。
	rec, _ = doJSON(t, h, http.MethodPost, "/v1/relations/REL-0001/conclude", payloadBody(events.RelationConcluded{
		SigningDate: "2026-02-01", EffectiveDate: "2026-03-01",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("缔结失败: %d %s", rec.Code, rec.Body.String())
	}

	// 逾期承诺（截止 2026-03-01，当前为 2026-09）。
	rec, _ = doJSON(t, h, http.MethodPost, "/v1/relations/REL-0001/commitments", payloadBody(events.CommitmentLogged{
		Ref: "CMT-0001", Category: domain.CatCulture,
		Description: domain.LocalizedText{"zh": "互办展览"}, ResponsibleDept: "文旅局", DueDate: "2026-03-01",
	}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("承诺登记失败: %d %s", rec.Code, rec.Body.String())
	}

	// 卷宗查询：状态 concluded、一项逾期、责任部门含外办与文旅局。
	rec, doss := doJSON(t, h, http.MethodGet, "/v1/relations/REL-0001", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("卷宗查询失败: %d", rec.Code)
	}
	rel := doss["relation"].(map[string]any)
	if rel["status"] != "concluded" {
		t.Fatalf("关系状态不符: %v", rel["status"])
	}
	overdue := doss["overdue_commitments"].([]any)
	if len(overdue) != 1 {
		t.Fatalf("应有一项逾期承诺，实得 %d", len(overdue))
	}
	depts := doss["responsible_departments"].([]any)
	deptSet := map[string]bool{}
	for _, d := range depts {
		deptSet[d.(string)] = true
	}
	if !deptSet["外办"] || !deptSet["文旅局"] {
		t.Fatalf("责任部门汇总不符: %v", depts)
	}
}

func TestBadRequestAndNotFound(t *testing.T) {
	h := newTestServer(t)
	// 非法 JSON。
	req := httptest.NewRequest(http.MethodPost, "/v1/entities", bytes.NewBufferString("{not-json"))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，实得 %d", rec.Code)
	}
	// 未知字段拒绝。
	rec, _ = doJSON(t, h, http.MethodPost, "/v1/entities", map[string]any{"payload": map[string]any{"unexpected": 1}})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应 400，实得 %d", rec.Code)
	}
	// 不存在对象 404。
	rec, _ = doJSON(t, h, http.MethodGet, "/v1/entities/ENT-ZZZZ", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在实体应 404，实得 %d", rec.Code)
	}
}

func TestProposalAnalysisEndpoint(t *testing.T) {
	h := newTestServer(t)
	doJSON(t, h, http.MethodPost, "/v1/entities", payloadBody(events.EntityRegistered{
		Ref: "ENT-AAAA", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "阿城"},
	}))
	doJSON(t, h, http.MethodPost, "/v1/entities", payloadBody(events.EntityRegistered{
		Ref: "ENT-BBBB", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "波城"},
	}))
	doJSON(t, h, http.MethodPost, "/v1/relations", payloadBody(events.RelationProposed{
		Ref: "REL-0001", EntityA: "ENT-AAAA", EntityB: "ENT-BBBB",
	}))
	rec, body := doJSON(t, h, http.MethodPost, "/v1/proposals/analyze", map[string]any{
		"entity_a": "ENT-AAAA", "entity_b": "ENT-BBBB",
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("提议分析失败: %d %s", rec.Code, rec.Body.String())
	}
	if body["existing_relation"] != "REL-0001" {
		t.Fatalf("应识别既有关系，实得 %v", body["existing_relation"])
	}
}

func TestIdempotentPostWithClientEventID(t *testing.T) {
	h := newTestServer(t)
	body := payloadBody(events.EntityRegistered{
		Ref: "ENT-IDEM", Level: domain.LevelCity, Names: domain.LocalizedText{"zh": "幂等城"},
	})
	body["meta"] = map[string]any{"event_id": "EVT-CLIENT01", "occurred_at": "2026-09-01T10:00:00+08:00"}
	rec1, _ := doJSON(t, h, http.MethodPost, "/v1/entities", body)
	rec2, _ := doJSON(t, h, http.MethodPost, "/v1/entities", body)
	if rec1.Code != http.StatusCreated || rec2.Code != http.StatusOK {
		t.Fatalf("同 event_id 重试应先 201 再幂等 200，实得 %d/%d", rec1.Code, rec2.Code)
	}
}
