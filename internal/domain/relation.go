package domain

import (
	"sort"
	"time"
)

// LegalApproval 保存一方法定批准记录。
type LegalApproval struct {
	PartyRef    string `json:"party_ref"`
	Authority   string `json:"authority,omitempty"`     // 法定批准机关（受控引用/名称）
	Instrument  string `json:"instrument,omitempty"`    // 批准文书受控引用
	ApprovedRaw string `json:"approved_at,omitempty"`   // 批准时间 RFC3339
	ApprovedAt  time.Time `json:"-"`
}

// TextVersion 保存关系文本（协议/备忘录）的一个版本。
// 多语原文、译文与最终签署版本同时保存；材料以受控引用 + sha256 摘要登记。
type TextVersion struct {
	Version       int           `json:"version"`
	Title         LocalizedText `json:"title"`
	BodyRef       string        `json:"body_ref,omitempty"`  // 正文材料受控引用
	BodyDigest    Digest        `json:"body_digest,omitempty"`
	Signed        bool          `json:"signed"`
	SignedRaw     string        `json:"signed_at,omitempty"` // 签署时间 RFC3339（跨时区保留偏移）
	SignedAt      time.Time     `json:"-"`
	SupersededBy  int           `json:"superseded_by,omitempty"`
}

// Relation 是两个行政实体之间的一对友好关系。
// 关系以无序双方唯一标识：重复上报不得创建第二对关系。
type Relation struct {
	Ref             string         `json:"ref"`
	PartyA          string         `json:"party_a"`
	PartyB          string         `json:"party_b"`
	Status          RelationStatus `json:"status"`
	EstablishedRaw  string         `json:"established_at,omitempty"` // 正式缔结/生效日期
	EstablishedAt   time.Time      `json:"-"`
	Approvals       []LegalApproval `json:"approvals"`
	TextVersions    []TextVersion   `json:"text_versions"`
	CurrentVersion  int             `json:"current_version,omitempty"`
	Categories      []CooperationCategory `json:"categories,omitempty"`
	// OriginPlanRef 记录该关系由哪份上级路线图/计划推动（可空）；上级路线图只约束不替代城市确认。
	OriginPlanRef   string         `json:"origin_plan_ref,omitempty"`
	// CityConfirmed 记录城市自身是否确认；即使存在上级路线图，也必须由城市确认后方可正式缔结。
	CityConfirmed   bool           `json:"city_confirmed"`
	StatusHistory   []StatusChange `json:"status_history"`
	CreatedRaw      string         `json:"created_at"`
	CreatedAt       time.Time      `json:"-"`
	UpdatedRaw      string         `json:"updated_at"`
	UpdatedAt       time.Time      `json:"-"`
}

// StatusChange 记录一次关系状态迁移。
type StatusChange struct {
	From      RelationStatus `json:"from,omitempty"`
	To        RelationStatus `json:"to"`
	AtRaw     string         `json:"at"`
	At        time.Time      `json:"-"`
	Reason    string         `json:"reason,omitempty"`
	EventID   string         `json:"event_id,omitempty"`
}

// 允许的关系状态迁移。意向可以补正后正式缔结；正式可暂停/终止；暂停可恢复或终止；终止为终态。
var transitions = map[RelationStatus]map[RelationStatus]bool{
	StatusIntended: {
		StatusConcluded: true,
		StatusTerminated: true,
	},
	StatusConcluded: {
		StatusSuspended: true,
		StatusTerminated: true,
	},
	StatusSuspended: {
		StatusConcluded: true, // 恢复
		StatusTerminated: true,
	},
	StatusTerminated: nil,
}

// CanTransition 报告状态迁移是否合法。
func CanTransition(from, to RelationStatus) bool {
	if from == to {
		return false
	}
	return transitions[from][to]
}

// PartyRefs 返回排序后的双方引用，作为无序唯一键的基础。
func PartyRefs(a, b string) [2]string {
	if a <= b {
		return [2]string{a, b}
	}
	return [2]string{b, a}
}

// PairKey 返回无序双方的稳定唯一键。
func PairKey(a, b string) string {
	p := PartyRefs(a, b)
	return p[0] + "|" + p[1]
}

// HasParty 报告某实体是否为关系一方。
func (r *Relation) HasParty(ref string) bool {
	return r.PartyA == ref || r.PartyB == ref
}

// OtherParty 返回对方引用。
func (r *Relation) OtherParty(ref string) string {
	if r.PartyA == ref {
		return r.PartyB
	}
	return r.PartyA
}

// ApprovalFor 返回某方的法定批准（若存在）。
func (r *Relation) ApprovalFor(partyRef string) (LegalApproval, bool) {
	for _, a := range r.Approvals {
		if a.PartyRef == partyRef {
			return a, true
		}
	}
	return LegalApproval{}, false
}

// LatestText 返回当前生效文本版本。
func (r *Relation) LatestText() (TextVersion, bool) {
	if len(r.TextVersions) == 0 {
		return TextVersion{}, false
	}
	sort.SliceStable(r.TextVersions, func(i, j int) bool {
		return r.TextVersions[i].Version < r.TextVersions[j].Version
	})
	return r.TextVersions[len(r.TextVersions)-1], true
}

// ReadyToConclude 报告是否满足正式缔结条件：双方法定批准齐备、存在签署文本、城市已确认。
// missing 返回尚未满足的条件说明，便于上层给出可核对的原因。
func (r *Relation) ReadyToConclude(isCity func(ref string) bool) (ready bool, missing []string) {
	if _, ok := r.ApprovalFor(r.PartyA); !ok {
		missing = append(missing, "缺少甲方 "+r.PartyA+" 的法定批准")
	}
	if _, ok := r.ApprovalFor(r.PartyB); !ok {
		missing = append(missing, "缺少乙方 "+r.PartyB+" 的法定批准")
	}
	latest, hasText := r.LatestText()
	if !hasText {
		missing = append(missing, "缺少关系文本版本")
	} else if !latest.Signed {
		missing = append(missing, "当前文本版本尚未签署")
	}
	if (isCity(r.PartyA) || isCity(r.PartyB)) && !r.CityConfirmed {
		missing = append(missing, "城市本身尚未确认（上级路线图不能替代）")
	}
	return len(missing) == 0, missing
}
