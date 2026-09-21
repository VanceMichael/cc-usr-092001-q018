package domain

import "time"

// Commitment 是关系项下双方约定推进的合作承诺事项。
// 同一关系内按 dedup_key 去重：重复上报不得创建第二条承诺。
type Commitment struct {
	Ref          string              `json:"ref"`
	RelationRef  string              `json:"relation_ref"`
	Category     CooperationCategory `json:"category"`
	Title        LocalizedText       `json:"title"`
	Description  LocalizedText       `json:"description,omitempty"`
	DedupKey     string              `json:"dedup_key"` // 规范化承诺指纹
	Status       CommitmentStatus    `json:"status"`
	// OwnerDept 是责任部门（受控引用），两侧可有各自责任部门。
	OwnerDepts   map[string]string   `json:"owner_depts,omitempty"` // party_ref -> 部门
	DueRaw       string              `json:"due_at,omitempty"`      // 截止时间 RFC3339（跨时区）
	DueAt        time.Time           `json:"-"`
	DoneRaw      string              `json:"done_at,omitempty"`
	DoneAt       time.Time           `json:"-"`
	SourcePlanRef string             `json:"source_plan_ref,omitempty"`
	CreatedRaw   string              `json:"created_at"`
	CreatedAt    time.Time           `json:"-"`
	UpdatedRaw   string              `json:"updated_at"`
	UpdatedAt    time.Time           `json:"-"`
}

// IsOverdueAt 按绝对时刻判断承诺在给定时间是否逾期（未完成且已过截止）。
func (c *Commitment) IsOverdueAt(now time.Time) bool {
	if c.Status == CommitFulfilled || c.Status == CommitCancelled {
		return false
	}
	return !c.DueAt.IsZero() && now.After(c.DueAt)
}

// Activity 是关系项下举办的活动及其成果。跨时区活动同时保存原始带偏移时间与绝对时刻。
type Activity struct {
	Ref            string              `json:"ref"`
	RelationRef    string              `json:"relation_ref"`
	CommitmentRef  string              `json:"commitment_ref,omitempty"`
	Category       CooperationCategory `json:"category"`
	Title          LocalizedText       `json:"title"`
	// StartRaw/EndRaw 保留主办方本地带偏移时间；StartAt/EndAt 为换算后的绝对时刻。
	StartRaw       string              `json:"start_at"`
	StartAt        time.Time           `json:"-"`
	EndRaw         string              `json:"end_at,omitempty"`
	EndAt          time.Time           `json:"-"`
	TimeZone       string              `json:"time_zone,omitempty"` // IANA 时区名，便于还原本地排期
	Location       string              `json:"location,omitempty"`
	HostPartyRef   string              `json:"host_party_ref,omitempty"`
	// Outcomes 保存活动成果（多语）与材料受控引用/摘要。
	Outcomes       []Outcome           `json:"outcomes,omitempty"`
	CreatedRaw     string              `json:"created_at"`
	CreatedAt      time.Time           `json:"-"`
}

// Outcome 是一项活动成果。
type Outcome struct {
	Summary   LocalizedText `json:"summary"`
	MaterialRef string      `json:"material_ref,omitempty"`
	Digest    Digest        `json:"digest,omitempty"`
}

// Plan 是上级路线图或下级交往计划。
// 层级上 country > state > city；上级路线图可约束下级计划，但不能自动替代城市本身的确认。
type Plan struct {
	Ref          string                `json:"ref"`
	Level        Level                 `json:"level"`
	Title        LocalizedText         `json:"title"`
	Status       PlanStatus            `json:"status"`
	// ParentRef 指向上级路线图（可空）。
	ParentRef    string                `json:"parent_ref,omitempty"`
	// RelationRef 指向该计划所约束/落实的友城关系（可空，国家级路线图通常不绑定单一城市对）。
	RelationRef  string                `json:"relation_ref,omitempty"`
	// AllowedCategories 约束下级计划可安排的合作领域；为空表示不限制。
	AllowedCategories []CooperationCategory `json:"allowed_categories,omitempty"`
	// CityConfirmed 是城市自身对本级计划的确认；由上级路线图派生的城市计划初始为 false。
	CityConfirmed bool                `json:"city_confirmed"`
	EffectiveRaw string               `json:"effective_at,omitempty"`
	EffectiveAt  time.Time            `json:"-"`
	ExpiryRaw    string               `json:"expiry_at,omitempty"`
	ExpiryAt     time.Time            `json:"-"`
	CreatedRaw   string               `json:"created_at"`
	CreatedAt    time.Time            `json:"-"`
	UpdatedRaw   string               `json:"updated_at"`
	UpdatedAt    time.Time            `json:"-"`
}

// AllowsCategory 报告在上级类别约束下能否安排某合作事项。
func (p *Plan) AllowsCategory(cat CooperationCategory) bool {
	if len(p.AllowedCategories) == 0 {
		return true
	}
	for _, c := range p.AllowedCategories {
		if c == cat {
			return true
		}
	}
	return false
}
