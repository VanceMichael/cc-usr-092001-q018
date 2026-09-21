package event

import "example.com/batch-092001-q018/internal/domain"

// ---- 行政实体 ----

type RegisterEntityPayload struct {
	Ref          string      `json:"ref"`
	Level        domain.Level `json:"level"`
	Jurisdiction string      `json:"jurisdiction,omitempty"`
	CurrentName  string      `json:"current_name"`
	Language     string      `json:"language,omitempty"`
}

func (p *RegisterEntityPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if !p.Level.Valid() {
		return domain.NewError(domain.CodeValidation, "payload.level=%q 不受控", p.Level)
	}
	if p.Jurisdiction != "" {
		if err := domain.ValidateRef(p.Jurisdiction, "payload.jurisdiction"); err != nil {
			return err
		}
	}
	if p.CurrentName == "" {
		return domain.NewError(domain.CodeValidation, "payload.current_name 不能为空")
	}
	return nil
}

type RenameEntityPayload struct {
	Ref      string `json:"ref"`
	NewName  string `json:"new_name"`
	Language string `json:"language,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

func (p *RenameEntityPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if p.NewName == "" {
		return domain.NewError(domain.CodeValidation, "payload.new_name 不能为空")
	}
	return nil
}

type AddAliasPayload struct {
	Ref      string `json:"ref"`
	Name     string `json:"name"`
	Language string `json:"language,omitempty"`
}

func (p *AddAliasPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if p.Name == "" {
		return domain.NewError(domain.CodeValidation, "payload.name 不能为空")
	}
	return nil
}

// MergeEntitiesPayload 将若干源实体合并进承接实体；旧实体保留为只读历史主体，旧关系不删除。
type MergeEntitiesPayload struct {
	SourceRefs []string `json:"source_refs"`
	TargetRef  string   `json:"target_ref"`
	Note       string   `json:"note,omitempty"`
}

func (p *MergeEntitiesPayload) Validate() error {
	if err := domain.ValidateRef(p.TargetRef, "payload.target_ref"); err != nil {
		return err
	}
	if len(p.SourceRefs) == 0 {
		return domain.NewError(domain.CodeValidation, "payload.source_refs 至少包含一个实体")
	}
	for _, r := range p.SourceRefs {
		if err := domain.ValidateRef(r, "payload.source_refs[]"); err != nil {
			return err
		}
		if r == p.TargetRef {
			return domain.NewError(domain.CodeValidation, "源实体不能与承接实体相同: %s", r)
		}
	}
	return nil
}

// ---- 友好关系 ----

type ProposeRelationPayload struct {
	Ref           string                        `json:"ref"`
	PartyA        string                        `json:"party_a"`
	PartyB        string                        `json:"party_b"`
	OriginPlanRef string                        `json:"origin_plan_ref,omitempty"`
	Categories    []domain.CooperationCategory  `json:"categories,omitempty"`
}

func (p *ProposeRelationPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if err := domain.ValidateRef(p.PartyA, "payload.party_a"); err != nil {
		return err
	}
	if err := domain.ValidateRef(p.PartyB, "payload.party_b"); err != nil {
		return err
	}
	if p.PartyA == p.PartyB {
		return domain.NewError(domain.CodeValidation, "友城关系双方不能相同: %s", p.PartyA)
	}
	if p.OriginPlanRef != "" {
		if err := domain.ValidateRef(p.OriginPlanRef, "payload.origin_plan_ref"); err != nil {
			return err
		}
	}
	for _, c := range p.Categories {
		if !c.Valid() {
			return domain.NewError(domain.CodeValidation, "payload.categories 含不受控类别 %q", c)
		}
	}
	return nil
}

type RecordApprovalPayload struct {
	RelationRef string `json:"relation_ref"`
	PartyRef    string `json:"party_ref"`
	Authority   string `json:"authority,omitempty"`
	Instrument  string `json:"instrument,omitempty"`
	ApprovedAt  string `json:"approved_at"`
}

func (p *RecordApprovalPayload) Validate() error {
	if err := domain.ValidateRef(p.RelationRef, "payload.relation_ref"); err != nil {
		return err
	}
	if err := domain.ValidateRef(p.PartyRef, "payload.party_ref"); err != nil {
		return err
	}
	if _, err := domain.ParseOccurredAt(p.ApprovedAt); err != nil {
		return domain.NewError(domain.CodeValidation, "payload.approved_at 必须为带偏移时间")
	}
	return nil
}

type RecordTextPayload struct {
	RelationRef string              `json:"relation_ref"`
	Title       domain.LocalizedText `json:"title"`
	BodyRef     string              `json:"body_ref,omitempty"`
	BodyDigest  domain.Digest       `json:"body_digest,omitempty"`
	Signed      bool                `json:"signed"`
	SignedAt    string              `json:"signed_at,omitempty"`
}

func (p *RecordTextPayload) Validate() error {
	if err := domain.ValidateRef(p.RelationRef, "payload.relation_ref"); err != nil {
		return err
	}
	if err := p.Title.Validate("payload.title"); err != nil {
		return err
	}
	if p.BodyDigest != "" {
		if err := p.BodyDigest.Validate("payload.body_digest"); err != nil {
			return err
		}
	}
	if p.Signed {
		if _, err := domain.ParseOccurredAt(p.SignedAt); err != nil {
			return domain.NewError(domain.CodeValidation, "签署文本必须提供带偏移的 signed_at")
		}
	}
	return nil
}

type ConfirmRelationCityPayload struct {
	RelationRef string `json:"relation_ref"`
	CityRef     string `json:"city_ref"`
}

func (p *ConfirmRelationCityPayload) Validate() error {
	if err := domain.ValidateRef(p.RelationRef, "payload.relation_ref"); err != nil {
		return err
	}
	return domain.ValidateRef(p.CityRef, "payload.city_ref")
}

type ConcludeRelationPayload struct {
	RelationRef string `json:"relation_ref"`
	EffectiveAt string `json:"effective_at"` // 生效日期（带偏移）
}

func (p *ConcludeRelationPayload) Validate() error {
	if err := domain.ValidateRef(p.RelationRef, "payload.relation_ref"); err != nil {
		return err
	}
	if _, err := domain.ParseOccurredAt(p.EffectiveAt); err != nil {
		return domain.NewError(domain.CodeValidation, "payload.effective_at 必须为带偏移时间")
	}
	return nil
}

type RelationStatusPayload struct {
	RelationRef string `json:"relation_ref"`
	At          string `json:"at"`
	Reason      string `json:"reason,omitempty"`
}

func (p *RelationStatusPayload) Validate() error {
	if err := domain.ValidateRef(p.RelationRef, "payload.relation_ref"); err != nil {
		return err
	}
	if _, err := domain.ParseOccurredAt(p.At); err != nil {
		return domain.NewError(domain.CodeValidation, "payload.at 必须为带偏移时间")
	}
	return nil
}

// ---- 路线图 / 交往计划 ----

type RegisterPlanPayload struct {
	Ref               string                        `json:"ref"`
	Level             domain.Level                  `json:"level"`
	Title             domain.LocalizedText          `json:"title"`
	ParentRef         string                        `json:"parent_ref,omitempty"`
	RelationRef       string                        `json:"relation_ref,omitempty"`
	AllowedCategories []domain.CooperationCategory   `json:"allowed_categories,omitempty"`
	CityConfirmed     bool                          `json:"city_confirmed"`
	EffectiveAt       string                        `json:"effective_at,omitempty"`
	ExpiryAt          string                        `json:"expiry_at,omitempty"`
}

func (p *RegisterPlanPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if !p.Level.Valid() {
		return domain.NewError(domain.CodeValidation, "payload.level=%q 不受控", p.Level)
	}
	if err := p.Title.Validate("payload.title"); err != nil {
		return err
	}
	if p.ParentRef != "" {
		if err := domain.ValidateRef(p.ParentRef, "payload.parent_ref"); err != nil {
			return err
		}
	}
	if p.RelationRef != "" {
		if err := domain.ValidateRef(p.RelationRef, "payload.relation_ref"); err != nil {
			return err
		}
	}
	for _, c := range p.AllowedCategories {
		if !c.Valid() {
			return domain.NewError(domain.CodeValidation, "payload.allowed_categories 含不受控类别 %q", c)
		}
	}
	if p.EffectiveAt != "" {
		if _, err := domain.ParseOccurredAt(p.EffectiveAt); err != nil {
			return domain.NewError(domain.CodeValidation, "payload.effective_at 必须为带偏移时间")
		}
	}
	if p.ExpiryAt != "" {
		if _, err := domain.ParseOccurredAt(p.ExpiryAt); err != nil {
			return domain.NewError(domain.CodeValidation, "payload.expiry_at 必须为带偏移时间")
		}
	}
	return nil
}

type PlanRefPayload struct {
	Ref string `json:"ref"`
}

func (p *PlanRefPayload) Validate() error { return domain.ValidateRef(p.Ref, "payload.ref") }

type BindPlanPayload struct {
	Ref         string `json:"ref"`
	EffectiveAt string `json:"effective_at,omitempty"`
}

func (p *BindPlanPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if p.EffectiveAt != "" {
		if _, err := domain.ParseOccurredAt(p.EffectiveAt); err != nil {
			return domain.NewError(domain.CodeValidation, "payload.effective_at 必须为带偏移时间")
		}
	}
	return nil
}

type SupersedePlanPayload struct {
	Ref           string `json:"ref"`
	SupersededBy  string `json:"superseded_by"`
}

func (p *SupersedePlanPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	return domain.ValidateRef(p.SupersededBy, "payload.superseded_by")
}

// ---- 承诺事项 ----

type RegisterCommitmentPayload struct {
	Ref           string                        `json:"ref"`
	RelationRef   string                        `json:"relation_ref"`
	Category      domain.CooperationCategory    `json:"category"`
	Title         domain.LocalizedText          `json:"title"`
	OwnerDepts    map[string]string             `json:"owner_depts,omitempty"`
	DueAt         string                        `json:"due_at,omitempty"`
	SourcePlanRef string                        `json:"source_plan_ref,omitempty"`
}

func (p *RegisterCommitmentPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if err := domain.ValidateRef(p.RelationRef, "payload.relation_ref"); err != nil {
		return err
	}
	if !p.Category.Valid() {
		return domain.NewError(domain.CodeValidation, "payload.category=%q 不受控", p.Category)
	}
	if err := p.Title.Validate("payload.title"); err != nil {
		return err
	}
	for party := range p.OwnerDepts {
		if err := domain.ValidateRef(party, "payload.owner_depts 键"); err != nil {
			return err
		}
	}
	if p.DueAt != "" {
		if _, err := domain.ParseOccurredAt(p.DueAt); err != nil {
			return domain.NewError(domain.CodeValidation, "payload.due_at 必须为带偏移时间")
		}
	}
	if p.SourcePlanRef != "" {
		if err := domain.ValidateRef(p.SourcePlanRef, "payload.source_plan_ref"); err != nil {
			return err
		}
	}
	return nil
}

type ProgressCommitmentPayload struct {
	Ref    string                `json:"ref"`
	Status domain.CommitmentStatus `json:"status"`
	At     string                `json:"at"`
}

func (p *ProgressCommitmentPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if p.Status != domain.CommitOpen && p.Status != domain.CommitInProgress {
		return domain.NewError(domain.CodeValidation, "进展事件只允许 open/in_progress 状态")
	}
	if _, err := domain.ParseOccurredAt(p.At); err != nil {
		return domain.NewError(domain.CodeValidation, "payload.at 必须为带偏移时间")
	}
	return nil
}

type FulfillCommitmentPayload struct {
	Ref string `json:"ref"`
	At  string `json:"at"`
}

func (p *FulfillCommitmentPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if _, err := domain.ParseOccurredAt(p.At); err != nil {
		return domain.NewError(domain.CodeValidation, "payload.at 必须为带偏移时间")
	}
	return nil
}

type CancelCommitmentPayload struct {
	Ref    string `json:"ref"`
	At     string `json:"at"`
	Reason string `json:"reason,omitempty"`
}

func (p *CancelCommitmentPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if _, err := domain.ParseOccurredAt(p.At); err != nil {
		return domain.NewError(domain.CodeValidation, "payload.at 必须为带偏移时间")
	}
	return nil
}

// ---- 活动与成果 ----

type RecordActivityPayload struct {
	Ref           string                        `json:"ref"`
	RelationRef   string                        `json:"relation_ref"`
	CommitmentRef string                        `json:"commitment_ref,omitempty"`
	Category      domain.CooperationCategory    `json:"category"`
	Title         domain.LocalizedText          `json:"title"`
	StartAt       string                        `json:"start_at"`
	EndAt         string                        `json:"end_at,omitempty"`
	TimeZone      string                        `json:"time_zone,omitempty"`
	Location      string                        `json:"location,omitempty"`
	HostPartyRef  string                        `json:"host_party_ref,omitempty"`
	Outcomes      []domain.Outcome              `json:"outcomes,omitempty"`
}

func (p *RecordActivityPayload) Validate() error {
	if err := domain.ValidateRef(p.Ref, "payload.ref"); err != nil {
		return err
	}
	if err := domain.ValidateRef(p.RelationRef, "payload.relation_ref"); err != nil {
		return err
	}
	if p.CommitmentRef != "" {
		if err := domain.ValidateRef(p.CommitmentRef, "payload.commitment_ref"); err != nil {
			return err
		}
	}
	if !p.Category.Valid() {
		return domain.NewError(domain.CodeValidation, "payload.category=%q 不受控", p.Category)
	}
	if err := p.Title.Validate("payload.title"); err != nil {
		return err
	}
	start, err := domain.ParseOccurredAt(p.StartAt)
	if err != nil {
		return domain.NewError(domain.CodeValidation, "payload.start_at 必须为带偏移时间")
	}
	if p.EndAt != "" {
		end, err := domain.ParseOccurredAt(p.EndAt)
		if err != nil {
			return domain.NewError(domain.CodeValidation, "payload.end_at 必须为带偏移时间")
		}
		if end.Before(start) {
			return domain.NewError(domain.CodeValidation, "活动结束时间早于开始时间")
		}
	}
	if p.HostPartyRef != "" {
		if err := domain.ValidateRef(p.HostPartyRef, "payload.host_party_ref"); err != nil {
			return err
		}
	}
	for i := range p.Outcomes {
		if err := p.Outcomes[i].Summary.Validate("payload.outcomes[].summary"); err != nil {
			return err
		}
		if p.Outcomes[i].Digest != "" {
			if err := p.Outcomes[i].Digest.Validate("payload.outcomes[].digest"); err != nil {
				return err
			}
		}
	}
	return nil
}
