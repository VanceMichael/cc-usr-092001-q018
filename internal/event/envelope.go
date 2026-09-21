package event

import (
	"encoding/json"
	"strings"

	"example.com/batch-092001-q018/internal/domain"
)

// 事件类型常量。所有写入图谱的变更都以类型化事件表达。
const (
	TypeEntityRegistered      = "entity.registered"
	TypeEntityRenamed         = "entity.renamed"
	TypeEntityMerged          = "entity.merged"
	TypeEntityAliasAdded      = "entity.alias_added"

	TypeRelationProposed      = "relation.proposed"
	TypeApprovalRecorded      = "relation.approval_recorded"
	TypeTextRecorded          = "relation.text_recorded"
	TypeRelationCityConfirmed = "relation.city_confirmed"
	TypeRelationConcluded     = "relation.concluded"
	TypeRelationSuspended     = "relation.suspended"
	TypeRelationResumed       = "relation.resumed"
	TypeRelationTerminated    = "relation.terminated"

	TypePlanRegistered        = "plan.registered"
	TypePlanBound             = "plan.bound"
	TypePlanSuperseded        = "plan.superseded"
	TypePlanWithdrawn         = "plan.withdrawn"
	TypePlanCityConfirmed     = "plan.city_confirmed"

	TypeCommitmentRegistered  = "commitment.registered"
	TypeCommitmentProgressed  = "commitment.progressed"
	TypeCommitmentFulfilled   = "commitment.fulfilled"
	TypeCommitmentCancelled   = "commitment.cancelled"

	TypeActivityRecorded      = "activity.recorded"
)

// Envelope 是外部交换事件的统一信封，字段形状见 contracts/。
type Envelope struct {
	SchemaVersion  string          `json:"schema_version"`
	EventID        string          `json:"event_id"`
	Source         string          `json:"source"` // 来源标识；source_sequence 仅在同一来源内递增
	EventType      string          `json:"event_type"`
	SubjectRef     string          `json:"subject_ref"`
	OccurredAt     string          `json:"occurred_at"`
	SourceSequence int64           `json:"source_sequence"`
	PayloadDigest  string          `json:"payload_digest"`
	Payload        json.RawMessage `json:"payload"`
}

// Validate 校验信封字段形状、时间、摘要并解析类型化载荷。
// 返回解码后的载荷；调用方再按事件类型做类型断言。
func (e *Envelope) Validate() (any, error) {
	if e.SchemaVersion != "1" {
		return nil, domain.NewError(domain.CodeValidation, "schema_version 必须为 %q，收到 %q", "1", e.SchemaVersion)
	}
	if err := domain.ValidateRef(e.EventID, "event_id"); err != nil {
		return nil, err
	}
	if strings.TrimSpace(e.Source) == "" {
		return nil, domain.NewError(domain.CodeValidation, "source 不能为空")
	}
	if strings.TrimSpace(e.SubjectRef) == "" {
		return nil, domain.NewError(domain.CodeValidation, "subject_ref 不能为空")
	}
	if e.SourceSequence <= 0 {
		return nil, domain.NewError(domain.CodeValidation, "source_sequence 必须为正整数")
	}
	if _, err := domain.ParseOccurredAt(e.OccurredAt); err != nil {
		return nil, err
	}
	if err := domain.Digest(e.PayloadDigest).Validate("payload_digest"); err != nil {
		return nil, err
	}
	if len(e.Payload) == 0 {
		return nil, domain.NewError(domain.CodeValidation, "payload 不能为空")
	}
	factory, ok := payloadFactories[e.EventType]
	if !ok {
		return nil, domain.NewError(domain.CodeValidation, "未知 event_type=%q", e.EventType)
	}
	payload := factory()
	if err := json.Unmarshal(e.Payload, payload); err != nil {
		return nil, domain.NewError(domain.CodeValidation, "payload 不是合法 JSON: %v", err)
	}
	// 摘要针对实际载荷字节校验，确保传输内容与登记摘要一致。
	got, err := CanonicalDigest(e.Payload)
	if err != nil {
		return nil, domain.NewError(domain.CodeValidation, "计算载荷摘要失败: %v", err)
	}
	if got != e.PayloadDigest {
		return nil, domain.NewError(domain.CodeDigestMismatch, "payload_digest 与载荷内容不符：登记=%s 实算=%s", e.PayloadDigest, got)
	}
	if v, ok := payload.(interface{ Validate() error }); ok {
		if err := v.Validate(); err != nil {
			return nil, err
		}
	}
	return payload, nil
}
