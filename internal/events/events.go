// Package events 定义图谱的事实事件。当前状态只能由事件重放得到，
// 业务规则不允许就地修改历史记录；行政区更名、合并、关系暂停或终止
// 都以新事件表达，旧事件保持不变，保证旧关系可追溯。
package events

import (
	"encoding/json"
	"fmt"
	"reflect"

	"example.com/batch-092001-q018/internal/domain"
)

// 载荷类型常量。
const (
	TypeEntityRegistered    = "entity_registered"
	TypeNameAttached        = "name_attached"
	TypeEntityMerged        = "entity_merged"
	TypeRelationProposed    = "relation_proposed"
	TypeRelationConcluded   = "relation_concluded"
	TypeRelationSuspended   = "relation_suspended"
	TypeRelationResumed     = "relation_resumed"
	TypeRelationTerminated  = "relation_terminated"
	TypeApprovalRecorded    = "approval_recorded"
	TypeTextRecorded        = "text_recorded"
	TypePlanAdopted         = "plan_adopted"
	TypePlanBoundToRelation = "plan_bound_to_relation"
	TypeCityPlanConfirmed   = "city_plan_confirmed"
	TypeCommitmentLogged    = "commitment_logged"
	TypeCommitmentFulfilled = "commitment_fulfilled"
	TypeActivityHeld        = "activity_held"
)

// Envelope 是落盘与对外交换的事件信封，字段与 contracts/event.example.json 对齐。
// Payload 使用原始 JSON 保存，摘要校验发生在解码之前。
type Envelope struct {
	SchemaVersion  string          `json:"schema_version"`
	EventID        string          `json:"event_id"`
	Source         string          `json:"source"`
	SourceSequence int64           `json:"source_sequence"`
	SubjectRef     string          `json:"subject_ref"`
	OccurredAt     string          `json:"occurred_at"`
	PayloadDigest  string          `json:"payload_digest"`
	Payload        json.RawMessage `json:"payload"`
}

// Payload 是所有事件载荷的共同接口。
type Payload interface {
	PayloadType() string
	// Validate 只做形状自检；跨对象的业务规则由 service 层负责。
	Validate() error
}

// EntityRegistered 建立国家、省州或城市的稳定标识。
type EntityRegistered struct {
	Ref       string               `json:"ref"`
	Level     domain.EntityLevel   `json:"level"`
	ParentRef string               `json:"parent_ref,omitempty"`
	Names     domain.LocalizedText `json:"names"`
}

func (e EntityRegistered) PayloadType() string { return TypeEntityRegistered }
func (e EntityRegistered) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixEntity); err != nil {
		return err
	}
	if !e.Level.Valid() {
		return fmt.Errorf("实体层级不受控: %q", e.Level)
	}
	if e.ParentRef != "" {
		if err := domain.CheckRef(e.ParentRef, domain.PrefixEntity); err != nil {
			return fmt.Errorf("上级实体引用不合法: %w", err)
		}
	}
	return domain.RequireText(e.Names, "实体名称")
}

// NameAttached 为实体追加名称（更名后的现名或旧名）。名称自身也有稳定标识，
// 历史名称随实体永久保留。
type NameAttached struct {
	NameRef    string               `json:"name_ref"`
	EntityRef  string               `json:"entity_ref"`
	Name       domain.LocalizedText `json:"name"`
	ValidFrom  string               `json:"valid_from"`           // YYYY-MM-DD
	Historical bool                 `json:"historical,omitempty"` // 追加时即为旧名（数据补录）
}

func (e NameAttached) PayloadType() string { return TypeNameAttached }
func (e NameAttached) Validate() error {
	if err := domain.CheckRef(e.NameRef, "NAM-"); err != nil {
		return err
	}
	if err := domain.CheckRef(e.EntityRef, domain.PrefixEntity); err != nil {
		return err
	}
	if err := domain.RequireText(e.Name, "名称"); err != nil {
		return err
	}
	_, err := domain.ParseDate(e.ValidFrom, "名称生效日期")
	return err
}

// EntityMerged 表达行政区合并：from 并入 into，from 的既有关系不移动、不删除，
// 通过继受链保持可追溯。
type EntityMerged struct {
	FromRef       string `json:"from_ref"`
	IntoRef       string `json:"into_ref"`
	EffectiveDate string `json:"effective_date"`
}

func (e EntityMerged) PayloadType() string { return TypeEntityMerged }
func (e EntityMerged) Validate() error {
	if err := domain.CheckRef(e.FromRef, domain.PrefixEntity); err != nil {
		return err
	}
	if err := domain.CheckRef(e.IntoRef, domain.PrefixEntity); err != nil {
		return err
	}
	if e.FromRef == e.IntoRef {
		return fmt.Errorf("合并的来源与目标不能相同")
	}
	_, err := domain.ParseDate(e.EffectiveDate, "合并生效日期")
	return err
}

// RelationProposed 登记一对行政实体之间的意向关系。
// 两侧顺序无关，重复上报同一对实体不会产生第二对关系。
type RelationProposed struct {
	Ref             string               `json:"ref"`
	EntityA         string               `json:"entity_a"`
	EntityB         string               `json:"entity_b"`
	Title           domain.LocalizedText `json:"title,omitempty"`
	ResponsibleDept string               `json:"responsible_dept,omitempty"`
}

func (e RelationProposed) PayloadType() string { return TypeRelationProposed }
func (e RelationProposed) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixRelation); err != nil {
		return err
	}
	if err := domain.CheckRef(e.EntityA, domain.PrefixEntity); err != nil {
		return err
	}
	if err := domain.CheckRef(e.EntityB, domain.PrefixEntity); err != nil {
		return err
	}
	if e.EntityA == e.EntityB {
		return fmt.Errorf("关系两侧不能为同一实体")
	}
	return nil
}

// RelationConcluded 将关系转为正式缔结。正式缔结要求双方法定批准齐备，
// 该规则在 service 层执行。
type RelationConcluded struct {
	Ref           string `json:"ref"`
	SigningDate   string `json:"signing_date"`   // YYYY-MM-DD
	EffectiveDate string `json:"effective_date"` // YYYY-MM-DD
}

func (e RelationConcluded) PayloadType() string { return TypeRelationConcluded }
func (e RelationConcluded) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixRelation); err != nil {
		return err
	}
	if _, err := domain.ParseDate(e.SigningDate, "签署日期"); err != nil {
		return err
	}
	_, err := domain.ParseDate(e.EffectiveDate, "生效日期")
	return err
}

// RelationSuspended 暂停关系。
type RelationSuspended struct {
	Ref    string `json:"ref"`
	At     string `json:"at"`
	Reason string `json:"reason,omitempty"`
}

func (e RelationSuspended) PayloadType() string { return TypeRelationSuspended }
func (e RelationSuspended) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixRelation); err != nil {
		return err
	}
	_, err := domain.ParseInstant(e.At, "暂停时间")
	return err
}

// RelationResumed 由暂停恢复为正式缔结。
type RelationResumed struct {
	Ref string `json:"ref"`
	At  string `json:"at"`
}

func (e RelationResumed) PayloadType() string { return TypeRelationResumed }
func (e RelationResumed) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixRelation); err != nil {
		return err
	}
	_, err := domain.ParseInstant(e.At, "恢复时间")
	return err
}

// RelationTerminated 终止关系；终止后历史承诺与文本仍可查阅。
type RelationTerminated struct {
	Ref    string `json:"ref"`
	At     string `json:"at"`
	Reason string `json:"reason,omitempty"`
}

func (e RelationTerminated) PayloadType() string { return TypeRelationTerminated }
func (e RelationTerminated) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixRelation); err != nil {
		return err
	}
	_, err := domain.ParseInstant(e.At, "终止时间")
	return err
}

// ApprovalRecorded 保存一方法定批准机关的决定。
type ApprovalRecorded struct {
	Ref              string                  `json:"ref"`
	RelationRef      string                  `json:"relation_ref"`
	Side             domain.ApprovalSide     `json:"side"`
	Decision         domain.ApprovalDecision `json:"decision"`
	Authority        string                  `json:"authority"`
	DecidedAt        string                  `json:"decided_at"`
	InstrumentDigest string                  `json:"instrument_digest,omitempty"` // sha256 摘要，受控引用
}

func (e ApprovalRecorded) PayloadType() string { return TypeApprovalRecorded }
func (e ApprovalRecorded) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixApproval); err != nil {
		return err
	}
	if err := domain.CheckRef(e.RelationRef, domain.PrefixRelation); err != nil {
		return err
	}
	if e.Side != domain.SideA && e.Side != domain.SideB {
		return fmt.Errorf("批准侧必须是 a 或 b")
	}
	if e.Decision != domain.ApprovalApproved && e.Decision != domain.ApprovalRejected {
		return fmt.Errorf("批准决定不受控")
	}
	if err := domain.RequireNonEmpty(e.Authority, "批准机关"); err != nil {
		return err
	}
	_, err := domain.ParseDate(e.DecidedAt, "批准日期")
	return err
}

// TextRecorded 保存关系文本的一个语言版本。原文、译文同构保存，
// SignedAt 非空表示该版本为签署版本。
type TextRecorded struct {
	Ref         string          `json:"ref"`
	RelationRef string          `json:"relation_ref"`
	Kind        domain.TextKind `json:"kind"`
	Language    string          `json:"language"`
	Content     string          `json:"content"`
	Version     int             `json:"version"`
	SignedAt    string          `json:"signed_at,omitempty"`
}

func (e TextRecorded) PayloadType() string { return TypeTextRecorded }
func (e TextRecorded) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixText); err != nil {
		return err
	}
	if err := domain.CheckRef(e.RelationRef, domain.PrefixRelation); err != nil {
		return err
	}
	if e.Kind != domain.TextOriginal && e.Kind != domain.TextTranslation {
		return fmt.Errorf("文本类型必须是 original 或 translation")
	}
	if err := domain.RequireNonEmpty(e.Language, "语言标签"); err != nil {
		return err
	}
	if err := domain.RequireNonEmpty(e.Content, "文本内容"); err != nil {
		return err
	}
	if e.Version < 1 {
		return fmt.Errorf("文本版本号必须从 1 开始")
	}
	if e.SignedAt != "" {
		if _, err := domain.ParseDate(e.SignedAt, "签署日期"); err != nil {
			return err
		}
	}
	return nil
}

// PlanAdopted 保存上级路线图或下级交往计划。下级计划通过 ParentRef
// 受上级路线图约束；约束不等于城市确认。
type PlanAdopted struct {
	Ref         string               `json:"ref"`
	Tier        domain.PlanTier      `json:"tier"`
	EntityA     string               `json:"entity_a"`
	EntityB     string               `json:"entity_b"`
	ParentRef   string               `json:"parent_ref,omitempty"`
	Title       domain.LocalizedText `json:"title"`
	AdoptedAt   string               `json:"adopted_at"`
	PeriodStart string               `json:"period_start,omitempty"`
	PeriodEnd   string               `json:"period_end,omitempty"`
}

func (e PlanAdopted) PayloadType() string { return TypePlanAdopted }
func (e PlanAdopted) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixPlan); err != nil {
		return err
	}
	if e.Tier != domain.PlanRoadmap && e.Tier != domain.PlanPlan {
		return fmt.Errorf("计划层级必须是 roadmap 或 plan")
	}
	if err := domain.CheckRef(e.EntityA, domain.PrefixEntity); err != nil {
		return err
	}
	if err := domain.CheckRef(e.EntityB, domain.PrefixEntity); err != nil {
		return err
	}
	if e.ParentRef != "" {
		if err := domain.CheckRef(e.ParentRef, domain.PrefixPlan); err != nil {
			return err
		}
	}
	if err := domain.RequireText(e.Title, "计划标题"); err != nil {
		return err
	}
	_, err := domain.ParseDate(e.AdoptedAt, "通过日期")
	return err
}

// PlanBoundToRelation 把计划（通常是上级路线图）挂接到一对下级关系上，
// 表达“上级文件约束该友城交往”。挂接不会代替城市自身的确认。
type PlanBoundToRelation struct {
	PlanRef     string `json:"plan_ref"`
	RelationRef string `json:"relation_ref"`
	At          string `json:"at"`
}

func (e PlanBoundToRelation) PayloadType() string { return TypePlanBoundToRelation }
func (e PlanBoundToRelation) Validate() error {
	if err := domain.CheckRef(e.PlanRef, domain.PrefixPlan); err != nil {
		return err
	}
	if err := domain.CheckRef(e.RelationRef, domain.PrefixRelation); err != nil {
		return err
	}
	_, err := domain.ParseInstant(e.At, "挂接时间")
	return err
}

// CityPlanConfirmed 是城市本身对受上级路线图约束的交往计划的确认。
// 只有该事件出现后，约束才在城市层面生效。
type CityPlanConfirmed struct {
	PlanRef     string `json:"plan_ref"`
	RelationRef string `json:"relation_ref"`
	CityRef     string `json:"city_ref"`
	At          string `json:"at"`
}

func (e CityPlanConfirmed) PayloadType() string { return TypeCityPlanConfirmed }
func (e CityPlanConfirmed) Validate() error {
	if err := domain.CheckRef(e.PlanRef, domain.PrefixPlan); err != nil {
		return err
	}
	if err := domain.CheckRef(e.RelationRef, domain.PrefixRelation); err != nil {
		return err
	}
	if err := domain.CheckRef(e.CityRef, domain.PrefixEntity); err != nil {
		return err
	}
	_, err := domain.ParseInstant(e.At, "确认时间")
	return err
}

// CommitmentLogged 登记一项合作承诺，带事项类别、责任部门与截止日期。
type CommitmentLogged struct {
	Ref             string               `json:"ref"`
	RelationRef     string               `json:"relation_ref"`
	Category        string               `json:"category"`
	Description     domain.LocalizedText `json:"description"`
	ResponsibleDept string               `json:"responsible_dept"`
	DueDate         string               `json:"due_date"` // YYYY-MM-DD
	PlanRef         string               `json:"plan_ref,omitempty"`
}

func (e CommitmentLogged) PayloadType() string { return TypeCommitmentLogged }
func (e CommitmentLogged) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixCommitment); err != nil {
		return err
	}
	if err := domain.CheckRef(e.RelationRef, domain.PrefixRelation); err != nil {
		return err
	}
	if !domain.ValidCategory(e.Category) {
		return fmt.Errorf("合作事项类别不受控: %q", e.Category)
	}
	if err := domain.RequireText(e.Description, "承诺描述"); err != nil {
		return err
	}
	if err := domain.RequireNonEmpty(e.ResponsibleDept, "责任部门"); err != nil {
		return err
	}
	if _, err := domain.ParseDate(e.DueDate, "截止日期"); err != nil {
		return err
	}
	if e.PlanRef != "" {
		if err := domain.CheckRef(e.PlanRef, domain.PrefixPlan); err != nil {
			return err
		}
	}
	return nil
}

// CommitmentFulfilled 记录承诺兑现（活动成果可回指承诺）。
type CommitmentFulfilled struct {
	Ref         string `json:"ref"` // 承诺引用
	FulfilledAt string `json:"fulfilled_at"`
	ActivityRef string `json:"activity_ref,omitempty"`
}

func (e CommitmentFulfilled) PayloadType() string { return TypeCommitmentFulfilled }
func (e CommitmentFulfilled) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixCommitment); err != nil {
		return err
	}
	_, err := domain.ParseDate(e.FulfilledAt, "兑现日期")
	return err
}

// ActivityHeld 记录跨时区交流活动及其成果。时刻保留带偏移量的原始字符串，
// 另存 IANA 时区名供展示；成果以多语文本保存原文与译文。
type ActivityHeld struct {
	Ref             string               `json:"ref"`
	RelationRef     string               `json:"relation_ref"`
	Category        string               `json:"category"`
	Title           domain.LocalizedText `json:"title"`
	StartAt         string               `json:"start_at"` // RFC3339，带偏移量
	EndAt           string               `json:"end_at,omitempty"`
	Timezone        string               `json:"timezone,omitempty"` // IANA，如 Asia/Shanghai
	Location        string               `json:"location,omitempty"`
	Outcomes        domain.LocalizedText `json:"outcomes,omitempty"`
	CommitmentRefs  []string             `json:"commitment_refs,omitempty"`
	ResponsibleDept string               `json:"responsible_dept,omitempty"`
}

func (e ActivityHeld) PayloadType() string { return TypeActivityHeld }
func (e ActivityHeld) Validate() error {
	if err := domain.CheckRef(e.Ref, domain.PrefixActivity); err != nil {
		return err
	}
	if err := domain.CheckRef(e.RelationRef, domain.PrefixRelation); err != nil {
		return err
	}
	if !domain.ValidCategory(e.Category) {
		return fmt.Errorf("合作事项类别不受控: %q", e.Category)
	}
	if err := domain.RequireText(e.Title, "活动标题"); err != nil {
		return err
	}
	start, err := domain.ParseInstant(e.StartAt, "活动开始时间")
	if err != nil {
		return err
	}
	if e.EndAt != "" {
		end, err := domain.ParseInstant(e.EndAt, "活动结束时间")
		if err != nil {
			return err
		}
		if end.Before(start) {
			return fmt.Errorf("活动结束时间早于开始时间")
		}
	}
	for _, ref := range e.CommitmentRefs {
		if err := domain.CheckRef(ref, domain.PrefixCommitment); err != nil {
			return fmt.Errorf("活动关联承诺不合法: %w", err)
		}
	}
	return nil
}

var payloadRegistry = map[string]func() Payload{
	TypeEntityRegistered:    func() Payload { return &EntityRegistered{} },
	TypeNameAttached:        func() Payload { return &NameAttached{} },
	TypeEntityMerged:        func() Payload { return &EntityMerged{} },
	TypeRelationProposed:    func() Payload { return &RelationProposed{} },
	TypeRelationConcluded:   func() Payload { return &RelationConcluded{} },
	TypeRelationSuspended:   func() Payload { return &RelationSuspended{} },
	TypeRelationResumed:     func() Payload { return &RelationResumed{} },
	TypeRelationTerminated:  func() Payload { return &RelationTerminated{} },
	TypeApprovalRecorded:    func() Payload { return &ApprovalRecorded{} },
	TypeTextRecorded:        func() Payload { return &TextRecorded{} },
	TypePlanAdopted:         func() Payload { return &PlanAdopted{} },
	TypePlanBoundToRelation: func() Payload { return &PlanBoundToRelation{} },
	TypeCityPlanConfirmed:   func() Payload { return &CityPlanConfirmed{} },
	TypeCommitmentLogged:    func() Payload { return &CommitmentLogged{} },
	TypeCommitmentFulfilled: func() Payload { return &CommitmentFulfilled{} },
	TypeActivityHeld:        func() Payload { return &ActivityHeld{} },
}

// DecodePayload 按类型字段解码载荷。类型字段随载荷以 "type" 键一起落盘。
func DecodePayload(raw json.RawMessage) (string, Payload, error) {
	var head struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &head); err != nil {
		return "", nil, fmt.Errorf("载荷不是合法 JSON: %w", err)
	}
	factory, ok := payloadRegistry[head.Type]
	if !ok {
		return "", nil, fmt.Errorf("未知事件类型: %q", head.Type)
	}
	ptr := factory()
	if err := json.Unmarshal(raw, ptr); err != nil {
		return "", nil, fmt.Errorf("解码载荷 %s 失败: %w", head.Type, err)
	}
	if err := ptr.Validate(); err != nil {
		return "", nil, fmt.Errorf("载荷 %s 自检失败: %w", head.Type, err)
	}
	// 返回值类型（而非指针），使消费方的类型断言与直接提交时保持一致。
	p, ok := reflect.ValueOf(ptr).Elem().Interface().(Payload)
	if !ok {
		return "", nil, fmt.Errorf("载荷 %s 未实现 Payload 接口", head.Type)
	}
	return head.Type, p, nil
}

// EncodePayload 进行编码（map 经 encoding/json 按键名排序输出，结果确定），
// 注入 "type" 鉴别字段，供摘要计算。
func EncodePayload(p Payload) (json.RawMessage, error) {
	if err := p.Validate(); err != nil {
		return nil, err
	}
	base, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	merged := make(map[string]json.RawMessage)
	if err := json.Unmarshal(base, &merged); err != nil {
		return nil, err
	}
	typeRaw, err := json.Marshal(p.PayloadType())
	if err != nil {
		return nil, err
	}
	merged["type"] = typeRaw
	return json.Marshal(merged)
}
