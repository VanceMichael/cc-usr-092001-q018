// Package api 提供图谱的 HTTP 接口。所有写操作都是提交领域事件：
// 载荷中携带 "type" 鉴别字段；同一 event_id 的重试幂等返回。
package api

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"example.com/batch-092001-q018/internal/events"
	"example.com/batch-092001-q018/internal/service"
)

// Handler 装配全部路由。
type Handler struct {
	svc *service.Service
}

// New 创建路由处理器。
func New(svc *service.Service) http.Handler {
	h := &Handler{svc: svc}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", h.health)

	// 事件交换
	mux.HandleFunc("POST /v1/events", h.ingestEvent)

	// 录入端点（服务端补齐事件标识与序号，亦可由调用方提供）
	mux.HandleFunc("POST /v1/entities", h.registerEntity)
	mux.HandleFunc("POST /v1/entities/{ref}/names", h.attachName)
	mux.HandleFunc("POST /v1/entities/merge", h.mergeEntities)
	mux.HandleFunc("POST /v1/relations", h.proposeRelation)
	mux.HandleFunc("POST /v1/relations/{ref}/approvals", h.recordApproval)
	mux.HandleFunc("POST /v1/relations/{ref}/conclude", h.concludeRelation)
	mux.HandleFunc("POST /v1/relations/{ref}/suspend", h.suspendRelation)
	mux.HandleFunc("POST /v1/relations/{ref}/resume", h.resumeRelation)
	mux.HandleFunc("POST /v1/relations/{ref}/terminate", h.terminateRelation)
	mux.HandleFunc("POST /v1/relations/{ref}/texts", h.recordText)
	mux.HandleFunc("POST /v1/plans", h.adoptPlan)
	mux.HandleFunc("POST /v1/plans/{ref}/bindings", h.bindPlan)
	mux.HandleFunc("POST /v1/plans/{ref}/confirmations", h.confirmCityPlan)
	mux.HandleFunc("POST /v1/relations/{ref}/commitments", h.logCommitment)
	mux.HandleFunc("POST /v1/commitments/{ref}/fulfill", h.fulfillCommitment)
	mux.HandleFunc("POST /v1/relations/{ref}/activities", h.holdActivity)

	// 查询端点
	mux.HandleFunc("GET /v1/entities", h.searchEntities)
	mux.HandleFunc("GET /v1/entities/{ref}", h.getEntity)
	mux.HandleFunc("GET /v1/relations", h.listRelations)
	mux.HandleFunc("GET /v1/relations/{ref}", h.getRelation)
	mux.HandleFunc("POST /v1/proposals/analyze", h.analyzeProposal)

	return logging(mux)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// envelopeRequest 允许录入端点同时携带交换元信息。
type envelopeRequest struct {
	Meta service.CommitMeta `json:"meta,omitempty"`
}

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "请求体不是合法 JSON: "+err.Error())
		return false
	}
	return true
}

func (h *Handler) commit(w http.ResponseWriter, r *http.Request, meta service.CommitMeta, p events.Payload) {
	result, err := h.svc.Commit(meta, p)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Duplicated {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

// ---- 事件交换 ----

func (h *Handler) ingestEvent(w http.ResponseWriter, r *http.Request) {
	var env events.Envelope
	if !decodeBody(w, r, &env) {
		return
	}
	result, err := h.svc.Ingest(env)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	status := http.StatusCreated
	if result.Duplicated {
		status = http.StatusOK
	}
	writeJSON(w, status, result)
}

// ---- 实体 ----

type registerEntityRequest struct {
	envelopeRequest
	Payload events.EntityRegistered `json:"payload"`
}

func (h *Handler) registerEntity(w http.ResponseWriter, r *http.Request) {
	var req registerEntityRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h.commit(w, r, req.Meta, req.Payload)
}

func (h *Handler) attachName(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Meta    service.CommitMeta  `json:"meta,omitempty"`
		Payload events.NameAttached `json:"payload"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Payload.EntityRef = r.PathValue("ref")
	h.commit(w, r, req.Meta, req.Payload)
}

type mergeRequest struct {
	envelopeRequest
	Payload events.EntityMerged `json:"payload"`
}

func (h *Handler) mergeEntities(w http.ResponseWriter, r *http.Request) {
	var req mergeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h.commit(w, r, req.Meta, req.Payload)
}

// ---- 关系 ----

type proposeRelationRequest struct {
	envelopeRequest
	Payload events.RelationProposed `json:"payload"`
}

func (h *Handler) proposeRelation(w http.ResponseWriter, r *http.Request) {
	var req proposeRelationRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h.commit(w, r, req.Meta, req.Payload)
}

func (h *Handler) recordApproval(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Meta    service.CommitMeta      `json:"meta,omitempty"`
		Payload events.ApprovalRecorded `json:"payload"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Payload.RelationRef = r.PathValue("ref")
	h.commit(w, r, req.Meta, req.Payload)
}

func (h *Handler) concludeRelation(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Meta    service.CommitMeta       `json:"meta,omitempty"`
		Payload events.RelationConcluded `json:"payload"`
	}
	if !decodeBody(w, r, &req) {
		return
	}
	req.Payload.Ref = r.PathValue("ref")
	h.commit(w, r, req.Meta, req.Payload)
}

type stateAtRequest struct {
	envelopeRequest
	Payload struct {
		At     string `json:"at"`
		Reason string `json:"reason,omitempty"`
	} `json:"payload"`
}

func (h *Handler) suspendRelation(w http.ResponseWriter, r *http.Request) {
	var req stateAtRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h.commit(w, r, req.Meta, events.RelationSuspended{
		Ref: r.PathValue("ref"), At: req.Payload.At, Reason: req.Payload.Reason,
	})
}

func (h *Handler) resumeRelation(w http.ResponseWriter, r *http.Request) {
	var req stateAtRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h.commit(w, r, req.Meta, events.RelationResumed{
		Ref: r.PathValue("ref"), At: req.Payload.At,
	})
}

func (h *Handler) terminateRelation(w http.ResponseWriter, r *http.Request) {
	var req stateAtRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h.commit(w, r, req.Meta, events.RelationTerminated{
		Ref: r.PathValue("ref"), At: req.Payload.At, Reason: req.Payload.Reason,
	})
}

type textRequest struct {
	envelopeRequest
	Payload events.TextRecorded `json:"payload"`
}

func (h *Handler) recordText(w http.ResponseWriter, r *http.Request) {
	var req textRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Payload.RelationRef = r.PathValue("ref")
	h.commit(w, r, req.Meta, req.Payload)
}

// ---- 计划 ----

type planRequest struct {
	envelopeRequest
	Payload events.PlanAdopted `json:"payload"`
}

func (h *Handler) adoptPlan(w http.ResponseWriter, r *http.Request) {
	var req planRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h.commit(w, r, req.Meta, req.Payload)
}

type bindingRequest struct {
	envelopeRequest
	Payload struct {
		RelationRef string `json:"relation_ref"`
		At          string `json:"at"`
	} `json:"payload"`
}

func (h *Handler) bindPlan(w http.ResponseWriter, r *http.Request) {
	var req bindingRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h.commit(w, r, req.Meta, events.PlanBoundToRelation{
		PlanRef:     r.PathValue("ref"),
		RelationRef: req.Payload.RelationRef,
		At:          req.Payload.At,
	})
}

type confirmationRequest struct {
	envelopeRequest
	Payload struct {
		RelationRef string `json:"relation_ref"`
		CityRef     string `json:"city_ref"`
		At          string `json:"at"`
	} `json:"payload"`
}

func (h *Handler) confirmCityPlan(w http.ResponseWriter, r *http.Request) {
	var req confirmationRequest
	if !decodeBody(w, r, &req) {
		return
	}
	h.commit(w, r, req.Meta, events.CityPlanConfirmed{
		PlanRef:     r.PathValue("ref"),
		RelationRef: req.Payload.RelationRef,
		CityRef:     req.Payload.CityRef,
		At:          req.Payload.At,
	})
}

// ---- 承诺与活动 ----

type commitmentRequest struct {
	envelopeRequest
	Payload events.CommitmentLogged `json:"payload"`
}

func (h *Handler) logCommitment(w http.ResponseWriter, r *http.Request) {
	var req commitmentRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Payload.RelationRef = r.PathValue("ref")
	h.commit(w, r, req.Meta, req.Payload)
}

type fulfillRequest struct {
	envelopeRequest
	Payload events.CommitmentFulfilled `json:"payload"`
}

func (h *Handler) fulfillCommitment(w http.ResponseWriter, r *http.Request) {
	var req fulfillRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Payload.Ref = r.PathValue("ref")
	h.commit(w, r, req.Meta, req.Payload)
}

type activityRequest struct {
	envelopeRequest
	Payload events.ActivityHeld `json:"payload"`
}

func (h *Handler) holdActivity(w http.ResponseWriter, r *http.Request) {
	var req activityRequest
	if !decodeBody(w, r, &req) {
		return
	}
	req.Payload.RelationRef = r.PathValue("ref")
	h.commit(w, r, req.Meta, req.Payload)
}

// ---- 查询 ----

func (h *Handler) getEntity(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.EntityDossier(r.PathValue("ref"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

func (h *Handler) searchEntities(w http.ResponseWriter, r *http.Request) {
	lang := r.URL.Query().Get("lang")
	name := r.URL.Query().Get("name")
	out, err := h.svc.SearchEntities(lang, name)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entities": out})
}

func (h *Handler) listRelations(w http.ResponseWriter, r *http.Request) {
	out, err := h.svc.ListRelations()
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"relations": out})
}

func (h *Handler) getRelation(w http.ResponseWriter, r *http.Request) {
	d, err := h.svc.RelationDossier(r.PathValue("ref"))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, d)
}

type proposalRequestHTTP struct {
	EntityA string `json:"entity_a"`
	EntityB string `json:"entity_b"`
}

func (h *Handler) analyzeProposal(w http.ResponseWriter, r *http.Request) {
	var req proposalRequestHTTP
	if !decodeBody(w, r, &req) {
		return
	}
	res, err := h.svc.AnalyzeProposal(req.EntityA, req.EntityB)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

// ---- 响应辅助 ----

type errorBody struct {
	Error string `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, errorBody{Error: message})
}

func writeServiceError(w http.ResponseWriter, err error) {
	var ve *service.ValidationError
	if errors.As(err, &ve) {
		writeError(w, http.StatusBadRequest, ve.Error())
		return
	}
	var nf *service.NotFoundError
	if errors.As(err, &nf) {
		writeError(w, http.StatusNotFound, nf.Error())
		return
	}
	var ce *service.ConflictError
	if errors.As(err, &ce) {
		writeError(w, http.StatusConflict, ce.Error())
		return
	}
	writeError(w, http.StatusInternalServerError, err.Error())
}

func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		log.Printf("%s %s", r.Method, r.URL.Path)
	})
}
