package httpapi

import (
	"encoding/json"
	"net/http"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/service"
)

// Handler 持有应用服务并暴露 HTTP 路由。
type Handler struct {
	svc *service.Service
}

// New 构造装配好路由的 http.Handler。
func New(svc *service.Service) http.Handler {
	h := &Handler{svc: svc}
	mux := http.NewServeMux()

	mux.HandleFunc("GET /health", h.health)
	mux.HandleFunc("POST /v1/events", h.ingestEvent)
	mux.HandleFunc("GET /v1/events", h.listEvents)
	mux.HandleFunc("GET /v1/entities/{ref}", h.getEntity)
	mux.HandleFunc("GET /v1/names/resolve", h.resolveName)
	mux.HandleFunc("GET /v1/relations/{ref}", h.getRelation)
	mux.HandleFunc("POST /v1/proposals:analyze", h.analyzeProposal)

	return loggingMiddleware(mux)
}

func (h *Handler) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) ingestEvent(w http.ResponseWriter, r *http.Request) {
	var env event.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		writeError(w, domain.NewError(domain.CodeValidation, "请求体不是合法 JSON: %v", err))
		return
	}
	res, err := h.svc.Ingest(env)
	if err != nil {
		writeError(w, err)
		return
	}
	status := http.StatusCreated
	if res.Idempotent {
		status = http.StatusOK
	}
	writeJSON(w, status, res)
}

func (h *Handler) listEvents(w http.ResponseWriter, r *http.Request) {
	limit := 0
	if v := r.URL.Query().Get("limit"); v != "" {
		if n := atoiPositive(v); n > 0 {
			limit = n
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": h.svc.ListEvents(limit)})
}

func (h *Handler) getEntity(w http.ResponseWriter, r *http.Request) {
	e, err := h.svc.GetEntity(r.PathValue("ref"))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (h *Handler) resolveName(w http.ResponseWriter, r *http.Request) {
	name := r.URL.Query().Get("name")
	if name == "" {
		writeError(w, domain.NewError(domain.CodeValidation, "查询参数 name 必填"))
		return
	}
	writeJSON(w, http.StatusOK, h.svc.ResolveName(name))
}

func (h *Handler) getRelation(w http.ResponseWriter, r *http.Request) {
	view, err := h.svc.GetRelationPair(r.PathValue("ref"), parseNow(r))
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (h *Handler) analyzeProposal(w http.ResponseWriter, r *http.Request) {
	var req service.AnalyzeProposalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, domain.NewError(domain.CodeValidation, "请求体不是合法 JSON: %v", err))
		return
	}
	out, err := h.svc.AnalyzeProposal(req)
	if err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}
