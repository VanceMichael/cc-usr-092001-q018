package httpapi

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"time"

	"example.com/batch-092001-q018/internal/domain"
)

// errorStatus 把领域错误码映射到 HTTP 状态码。
func errorStatus(code domain.ErrorCode) int {
	switch code {
	case domain.CodeValidation, domain.CodeDigestMismatch, domain.CodeTextVersion:
		return http.StatusBadRequest
	case domain.CodeUnknownEntity, domain.CodeUnknownRelation, domain.CodeUnknownPlan,
		domain.CodeUnknownCommitment:
		return http.StatusNotFound
	case domain.CodeDuplicateEvent, domain.CodeSequenceConflict, domain.CodeDuplicateRelation,
		domain.CodeDuplicateCommitment:
		return http.StatusConflict
	case domain.CodePartyMerged:
		return http.StatusGone
	case domain.CodeInvalidTransition, domain.CodePlanConstraint:
		return http.StatusConflict
	case domain.CodeMissingApproval, domain.CodeCityConfirmationMissing:
		return http.StatusUnprocessableEntity
	case domain.CodeReference:
		return http.StatusConflict
	default:
		return http.StatusInternalServerError
	}
}

func writeError(w http.ResponseWriter, err error) {
	if de, ok := err.(*domain.Error); ok {
		writeJSON(w, errorStatus(de.Code), map[string]any{"error": de})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{
		"error": domain.Error{Code: "INTERNAL", Message: err.Error()},
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func atoiPositive(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// parseNow 解析可选的 at 查询参数，便于逾期判定可复现；缺省由服务取当前时刻。
func parseNow(r *http.Request) time.Time {
	v := r.URL.Query().Get("at")
	if v == "" {
		return time.Time{}
	}
	t, err := domain.ParseOccurredAt(v)
	if err != nil {
		return time.Time{}
	}
	return t
}

// statusWriter 捕获响应状态码用于访问日志。
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(sw, r)
		log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, sw.status, time.Since(start))
	})
}
