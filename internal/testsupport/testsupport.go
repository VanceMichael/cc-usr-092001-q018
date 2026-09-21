package testsupport

import (
	"encoding/json"
	"strconv"
	"testing"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/service"
)

var seq int64

// Envelope 用正确的 payload_digest 构造一个事件信封，供各测试复用。
// sourceSequence 为 0 时自动在固定来源内递增。
func Envelope(t *testing.T, eventID, eventType, subject, occurredAt string, payload any) event.Envelope {
	t.Helper()
	return EnvelopeSrc(t, eventID, "SRC-018-TEST", 0, eventType, subject, occurredAt, payload)
}

// EnvelopeSrc 允许指定来源与序号。
func EnvelopeSrc(t *testing.T, eventID, source string, sourceSequence int64, eventType, subject, occurredAt string, payload any) event.Envelope {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("编码 payload 失败: %v", err)
	}
	digest, err := event.CanonicalDigest(raw)
	if err != nil {
		t.Fatalf("计算摘要失败: %v", err)
	}
	if sourceSequence == 0 {
		seq++
		sourceSequence = seq
	}
	return event.Envelope{
		SchemaVersion:  "1",
		EventID:        eventID,
		Source:         source,
		EventType:      eventType,
		SubjectRef:     subject,
		OccurredAt:     occurredAt,
		SourceSequence: sourceSequence,
		PayloadDigest:  digest,
		Payload:        raw,
	}
}

// ResetSeq 重置自动序号（每个独立测试调用）。
func ResetSeq() { seq = 0 }

// EventID 返回形如 EVT-000001 的合规事件引用编号。
func EventID() string {
	seq++
	return "EVT-" + pad(seq)
}

func pad(n int64) string {
	s := strconv.FormatInt(n, 10)
	for len(s) < 6 {
		s = "0" + s
	}
	return s
}

// Ingester 是服务受理接口的最小约束。
type Ingester interface {
	Ingest(event.Envelope) (service.IngestResult, error)
}

// MustIngest 受理事件并在失败时终止测试。
func MustIngest(t *testing.T, svc Ingester, env event.Envelope) {
	t.Helper()
	if _, err := svc.Ingest(env); err != nil {
		t.Fatalf("受理事件 %s(%s) 失败: %v", env.EventID, env.EventType, err)
	}
}

// FailIngest 受理事件并断言其以指定错误码失败。
func FailIngest(t *testing.T, svc Ingester, env event.Envelope, wantCode domain.ErrorCode) {
	t.Helper()
	_, err := svc.Ingest(env)
	if err == nil {
		t.Fatalf("事件 %s(%s) 应当失败但被受理", env.EventID, env.EventType)
	}
	AssertCode(t, err, wantCode)
}

// AssertCode 断言错误携带指定领域错误码。
func AssertCode(t *testing.T, err error, wantCode domain.ErrorCode) {
	t.Helper()
	de, ok := err.(*domain.Error)
	if !ok {
		t.Fatalf("期望领域错误码 %s，得到非领域错误: %v", wantCode, err)
	}
	if de.Code != wantCode {
		t.Fatalf("期望错误码 %s，得到 %s（%s）", wantCode, de.Code, de.Message)
	}
}
