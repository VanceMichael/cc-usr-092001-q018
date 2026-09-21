package service

import (
	"time"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/store"
)

// Projector 把类型化事件应用到图谱投影，承载全部写入期业务规则。
// 调用方（Service）已串行化写入，本结构的方法在内部获取图锁。
type Projector struct {
	graph *store.Graph
}

// NewProjector 创建投影应用器。
func NewProjector(g *store.Graph) *Projector { return &Projector{graph: g} }

// Apply 校验并应用单条已接纳事件。任何规则失败都返回错误且不产生部分变更
// （各事件处理函数在完成全部检查后才写入）。
func (p *Projector) Apply(env event.Envelope, payload any) error {
	g := p.graph
	g.Lock()
	defer g.Unlock()

	at, _ := domain.ParseOccurredAt(env.OccurredAt) // 信封已校验
	switch pl := payload.(type) {
	// ---- 实体 ----
	case *event.RegisterEntityPayload:
		return p.applyRegisterEntity(pl, env.OccurredAt, at)
	case *event.RenameEntityPayload:
		return p.applyRename(pl, env.OccurredAt, at)
	case *event.AddAliasPayload:
		return p.applyAddAlias(pl, at)
	case *event.MergeEntitiesPayload:
		return p.applyMerge(pl, env.OccurredAt, at)

	// ---- 关系 ----
	case *event.ProposeRelationPayload:
		return p.applyProposeRelation(pl, env.OccurredAt, at)
	case *event.RecordApprovalPayload:
		return p.applyApproval(pl, at)
	case *event.RecordTextPayload:
		return p.applyText(pl, at)
	case *event.ConfirmRelationCityPayload:
		return p.applyRelationCityConfirm(pl)
	case *event.ConcludeRelationPayload:
		return p.applyConclude(pl, at, env.EventID)
	case *event.RelationStatusPayload:
		return p.applyRelationStatus(env.EventType, pl, at, env.EventID)

	// ---- 计划 ----
	case *event.RegisterPlanPayload:
		return p.applyRegisterPlan(pl, at)
	case *event.BindPlanPayload:
		return p.applyBindPlan(pl, at)
	case *event.SupersedePlanPayload:
		return p.applySupersedePlan(pl)
	case *event.PlanRefPayload:
		return p.applyPlanRefAction(env.EventType, pl)

	// ---- 承诺 ----
	case *event.RegisterCommitmentPayload:
		return p.applyRegisterCommitment(pl, at)
	case *event.ProgressCommitmentPayload:
		return p.applyProgressCommitment(pl, at)
	case *event.FulfillCommitmentPayload:
		return p.applyFulfillCommitment(pl, at)
	case *event.CancelCommitmentPayload:
		return p.applyCancelCommitment(pl, at)

	// ---- 活动 ----
	case *event.RecordActivityPayload:
		return p.applyRecordActivity(pl, at)

	default:
		return domain.NewError(domain.CodeValidation, "未注册的载荷类型 %T", payload)
	}
}

func (p *Projector) requireEntity(ref string) (*domain.Entity, error) {
	e, ok := p.graph.Entity(ref)
	if !ok {
		return nil, domain.NewError(domain.CodeUnknownEntity, "实体 %s 不存在", ref)
	}
	return e, nil
}

func (p *Projector) requireRelation(ref string) (*domain.Relation, error) {
	r, ok := p.graph.Relation(ref)
	if !ok {
		return nil, domain.NewError(domain.CodeUnknownRelation, "关系 %s 不存在", ref)
	}
	return r, nil
}

func (p *Projector) requirePlan(ref string) (*domain.Plan, error) {
	pl, ok := p.graph.Plan(ref)
	if !ok {
		return nil, domain.NewError(domain.CodeUnknownPlan, "计划 %s 不存在", ref)
	}
	return pl, nil
}

// requireActiveEntity 要求实体存在且未被合并；被合并时指引到承接实体。
func (p *Projector) requireActiveEntity(ref string) (*domain.Entity, error) {
	e, err := p.requireEntity(ref)
	if err != nil {
		return nil, err
	}
	if e.MergedInto != "" {
		cur := p.graph.ResolveEntity(ref)
		suggest := ref
		if cur != nil {
			suggest = cur.Ref
		}
		return nil, domain.NewError(domain.CodePartyMerged,
			"实体 %s 已在行政区调整中并入 %s，新记录请引用承接实体", ref, suggest)
	}
	return e, nil
}

// ---- 实体事件 ----

func (p *Projector) applyRegisterEntity(pl *event.RegisterEntityPayload, raw string, at time.Time) error {
	if _, exists := p.graph.Entity(pl.Ref); exists {
		return domain.NewError(domain.CodeReference, "实体 %s 已注册，不得重复登记", pl.Ref)
	}
	if pl.Jurisdiction != "" {
		if _, ok := p.graph.Entity(pl.Jurisdiction); !ok {
			return domain.NewError(domain.CodeUnknownEntity, "上级辖区 %s 不存在", pl.Jurisdiction)
		}
	}
	// 同名已存在实体不直接拒绝，但交由提议分析消歧；此处仅在同层级同辖区给出冲突提示。
	e := &domain.Entity{
		Ref: pl.Ref, Level: pl.Level, Jurisdiction: pl.Jurisdiction,
		CurrentName: pl.CurrentName, CreatedRaw: raw, CreatedAt: at,
		UpdatedRaw: raw, UpdatedAt: at,
	}
	e.AddName(pl.CurrentName, pl.Language, raw, at, "register", true)
	p.graph.PutEntity(e)
	return nil
}

func (p *Projector) applyRename(pl *event.RenameEntityPayload, raw string, at time.Time) error {
	e, err := p.requireActiveEntity(pl.Ref)
	if err != nil {
		return err
	}
	if domain.NormalizeName(e.CurrentName) == domain.NormalizeName(pl.NewName) {
		return domain.NewError(domain.CodeValidation, "实体 %s 当前名称已是 %q", pl.Ref, pl.NewName)
	}
	reason := pl.Reason
	if reason == "" {
		reason = "rename"
	}
	e.AddName(pl.NewName, pl.Language, raw, at, reason, true)
	e.CurrentName = pl.NewName
	e.UpdatedRaw, e.UpdatedAt = raw, at
	// 旧名保留在 e.Names（Current=false），旧关系仍可追溯。
	p.graph.PutEntity(e)
	return nil
}

func (p *Projector) applyAddAlias(pl *event.AddAliasPayload, at time.Time) error {
	e, err := p.requireEntity(pl.Ref)
	if err != nil {
		return err
	}
	e.AddName(pl.Name, pl.Language, "", at, "alias", false)
	p.graph.PutEntity(e)
	return nil
}

func (p *Projector) applyMerge(pl *event.MergeEntitiesPayload, raw string, at time.Time) error {
	target, err := p.requireActiveEntity(pl.TargetRef)
	if err != nil {
		return err
	}
	sources := make([]*domain.Entity, 0, len(pl.SourceRefs))
	for _, ref := range pl.SourceRefs {
		s, err := p.requireActiveEntity(ref)
		if err != nil {
			return err
		}
		sources = append(sources, s)
	}
	reorg := domain.AdministrativeReorg{
		Kind: "merge", SourceRefs: pl.SourceRefs, TargetRef: pl.TargetRef,
		Effective: raw, Note: pl.Note,
	}
	for _, s := range sources {
		s.MergedInto = pl.TargetRef
		s.Reorgs = append(s.Reorgs, reorg)
		s.UpdatedRaw, s.UpdatedAt = raw, at
		// 旧实体的当前名与历史名作为合并别名落到承接实体，保持按旧名可检索、旧关系可追溯。
		s.AddName(s.CurrentName, "", raw, at, "merge", false)
		alias := *s
		for _, n := range alias.Names {
			target.AddName(n.Name, n.Language, n.FromRaw, n.From, "merge", false)
		}
		target.AddName(s.CurrentName, "", raw, at, "merge", false)
		p.graph.PutEntity(s)
	}
	target.Reorgs = append(target.Reorgs, reorg)
	target.UpdatedRaw, target.UpdatedAt = raw, at
	p.graph.PutEntity(target)
	return nil
}

// ---- 关系事件 ----

func (p *Projector) applyProposeRelation(pl *event.ProposeRelationPayload, raw string, at time.Time) error {
	a, err := p.requireActiveEntity(pl.PartyA)
	if err != nil {
		return err
	}
	b, err := p.requireActiveEntity(pl.PartyB)
	if err != nil {
		return err
	}
	if pl.OriginPlanRef != "" {
		plan, err := p.requirePlan(pl.OriginPlanRef)
		if err != nil {
			return err
		}
		_ = plan
	}
	if _, exists := p.graph.Relation(pl.Ref); exists {
		return domain.NewError(domain.CodeReference, "关系引用 %s 已存在", pl.Ref)
	}
	if _, existing, ok := p.graph.RelationByPair(a.Ref, b.Ref); ok {
		return domain.NewError(domain.CodeDuplicateRelation,
			"双方 %s 与 %s 已存在关系 %s（状态=%s），重复上报不得创建第二对关系",
			a.Ref, b.Ref, existing.Ref, existing.Status)
	}
	cats := dedupCategories(pl.Categories)
	r := &domain.Relation{
		Ref: pl.Ref, PartyA: a.Ref, PartyB: b.Ref, Status: domain.StatusIntended,
		OriginPlanRef: pl.OriginPlanRef, Categories: cats,
		CreatedRaw: raw, CreatedAt: at, UpdatedRaw: raw, UpdatedAt: at,
		StatusHistory: []domain.StatusChange{{To: domain.StatusIntended, AtRaw: raw, At: at}},
	}
	p.graph.PutRelation(r)
	return nil
}

func (p *Projector) applyApproval(pl *event.RecordApprovalPayload, at time.Time) error {
	r, err := p.requireRelation(pl.RelationRef)
	if err != nil {
		return err
	}
	if !r.HasParty(pl.PartyRef) {
		return domain.NewError(domain.CodeReference, "实体 %s 不是关系 %s 的一方", pl.PartyRef, pl.RelationRef)
	}
	if _, err := p.requireEntity(pl.PartyRef); err != nil {
		return err
	}
	approval := domain.LegalApproval{
		PartyRef: pl.PartyRef, Authority: pl.Authority, Instrument: pl.Instrument,
		ApprovedRaw: pl.ApprovedAt, ApprovedAt: at,
	}
	replaced := false
	for i := range r.Approvals {
		if r.Approvals[i].PartyRef == pl.PartyRef {
			r.Approvals[i] = approval // 同一方的更新批文覆盖旧记录
			replaced = true
			break
		}
	}
	if !replaced {
		r.Approvals = append(r.Approvals, approval)
	}
	r.UpdatedRaw, r.UpdatedAt = pl.ApprovedAt, at
	p.graph.PutRelation(r)
	return nil
}

func (p *Projector) applyText(pl *event.RecordTextPayload, at time.Time) error {
	r, err := p.requireRelation(pl.RelationRef)
	if err != nil {
		return err
	}
	version := len(r.TextVersions) + 1
	tv := domain.TextVersion{
		Version: version, Title: pl.Title, BodyRef: pl.BodyRef, BodyDigest: pl.BodyDigest,
		Signed: pl.Signed,
	}
	if pl.Signed {
		tv.SignedRaw, tv.SignedAt = pl.SignedAt, at
	}
	r.TextVersions = append(r.TextVersions, tv)
	r.CurrentVersion = version
	if pl.SignedAt != "" {
		r.UpdatedRaw, r.UpdatedAt = pl.SignedAt, at
	}
	p.graph.PutRelation(r)
	return nil
}

func (p *Projector) applyRelationCityConfirm(pl *event.ConfirmRelationCityPayload) error {
	r, err := p.requireRelation(pl.RelationRef)
	if err != nil {
		return err
	}
	if !r.HasParty(pl.CityRef) {
		return domain.NewError(domain.CodeReference, "城市 %s 不是关系 %s 的一方", pl.CityRef, pl.RelationRef)
	}
	city, err := p.requireEntity(pl.CityRef)
	if err != nil {
		return err
	}
	if city.Level != domain.LevelCity {
		return domain.NewError(domain.CodeValidation, "实体 %s 层级为 %s，不是城市，不能作城市确认", pl.CityRef, city.Level)
	}
	r.CityConfirmed = true
	p.graph.PutRelation(r)
	return nil
}

func (p *Projector) isCity(ref string) bool {
	if e := p.graph.ResolveEntity(ref); e != nil {
		return e.Level == domain.LevelCity
	}
	return false
}

func (p *Projector) applyConclude(pl *event.ConcludeRelationPayload, at time.Time, eventID string) error {
	r, err := p.requireRelation(pl.RelationRef)
	if err != nil {
		return err
	}
	if r.Status != domain.StatusIntended {
		return domain.NewError(domain.CodeInvalidTransition,
			"关系 %s 当前状态=%s，仅意向关系可正式缔结", r.Ref, r.Status)
	}
	ready, missing := r.ReadyToConclude(p.isCity)
	if !ready {
		return domain.NewError(domain.CodeMissingApproval,
			"关系 %s 尚不满足正式缔结条件：%s", r.Ref, joinReasons(missing))
	}
	r.Status = domain.StatusConcluded
	r.EstablishedRaw, r.EstablishedAt = pl.EffectiveAt, at
	r.UpdatedRaw, r.UpdatedAt = pl.EffectiveAt, at
	r.StatusHistory = append(r.StatusHistory, domain.StatusChange{
		From: domain.StatusIntended, To: domain.StatusConcluded,
		AtRaw: pl.EffectiveAt, At: at, EventID: eventID,
	})
	p.graph.PutRelation(r)
	return nil
}

func (p *Projector) applyRelationStatus(eventType string, pl *event.RelationStatusPayload, at time.Time, eventID string) error {
	r, err := p.requireRelation(pl.RelationRef)
	if err != nil {
		return err
	}
	var target domain.RelationStatus
	switch eventType {
	case event.TypeRelationSuspended:
		target = domain.StatusSuspended
	case event.TypeRelationResumed:
		target = domain.StatusConcluded
	case event.TypeRelationTerminated:
		target = domain.StatusTerminated
	default:
		return domain.NewError(domain.CodeValidation, "事件类型 %s 不改变关系状态", eventType)
	}
	if !domain.CanTransition(r.Status, target) {
		return domain.NewError(domain.CodeInvalidTransition,
			"关系 %s 不允许从 %s 迁移到 %s", r.Ref, r.Status, target)
	}
	from := r.Status
	r.Status = target
	r.UpdatedRaw, r.UpdatedAt = pl.At, at
	r.StatusHistory = append(r.StatusHistory, domain.StatusChange{
		From: from, To: target, AtRaw: pl.At, At: at, Reason: pl.Reason, EventID: eventID,
	})
	p.graph.PutRelation(r)
	return nil
}

// ---- 计划事件 ----

func levelRank(l domain.Level) int {
	switch l {
	case domain.LevelCountry:
		return 0
	case domain.LevelState:
		return 1
	default:
		return 2
	}
}

func (p *Projector) applyRegisterPlan(pl *event.RegisterPlanPayload, at time.Time) error {
	if _, exists := p.graph.Plan(pl.Ref); exists {
		return domain.NewError(domain.CodeReference, "计划引用 %s 已存在", pl.Ref)
	}
	if pl.ParentRef != "" {
		parent, err := p.requirePlan(pl.ParentRef)
		if err != nil {
			return err
		}
		if levelRank(pl.Level) <= levelRank(parent.Level) {
			return domain.NewError(domain.CodeValidation,
				"计划 %s 层级 %s 必须低于上级路线图 %s 的层级 %s",
				pl.Ref, pl.Level, parent.Ref, parent.Level)
		}
	}
	if pl.RelationRef != "" {
		if _, err := p.requireRelation(pl.RelationRef); err != nil {
			return err
		}
	}
	var effAt, expAt time.Time
	if pl.EffectiveAt != "" {
		effAt, _ = domain.ParseOccurredAt(pl.EffectiveAt)
	}
	if pl.ExpiryAt != "" {
		expAt, _ = domain.ParseOccurredAt(pl.ExpiryAt)
		if !effAt.IsZero() && expAt.Before(effAt) {
			return domain.NewError(domain.CodeValidation, "计划失效时间早于生效时间")
		}
	}
	plan := &domain.Plan{
		Ref: pl.Ref, Level: pl.Level, Title: pl.Title, Status: domain.PlanProposed,
		ParentRef: pl.ParentRef, RelationRef: pl.RelationRef,
		AllowedCategories: dedupCategories(pl.AllowedCategories),
		CityConfirmed: pl.CityConfirmed,
		EffectiveRaw: pl.EffectiveAt, EffectiveAt: effAt,
		ExpiryRaw: pl.ExpiryAt, ExpiryAt: expAt,
		CreatedRaw: pl.EffectiveAt, CreatedAt: at, UpdatedAt: at,
	}
	p.graph.PutPlan(plan)
	return nil
}

func (p *Projector) applyBindPlan(pl *event.BindPlanPayload, at time.Time) error {
	plan, err := p.requirePlan(pl.Ref)
	if err != nil {
		return err
	}
	if plan.Status != domain.PlanProposed {
		return domain.NewError(domain.CodeInvalidTransition,
			"计划 %s 状态=%s，仅 proposed 可生效为 binding", plan.Ref, plan.Status)
	}
	plan.Status = domain.PlanBinding
	if pl.EffectiveAt != "" {
		plan.EffectiveRaw = pl.EffectiveAt
		plan.EffectiveAt, _ = domain.ParseOccurredAt(pl.EffectiveAt)
	} else if plan.EffectiveRaw == "" {
		plan.EffectiveRaw = rawTime(at)
		plan.EffectiveAt = at
	}
	plan.UpdatedAt = at
	p.graph.PutPlan(plan)
	return nil
}

func (p *Projector) applySupersedePlan(pl *event.SupersedePlanPayload) error {
	plan, err := p.requirePlan(pl.Ref)
	if err != nil {
		return err
	}
	if _, err := p.requirePlan(pl.SupersededBy); err != nil {
		return err
	}
	if plan.Status == domain.PlanSuperseded || plan.Status == domain.PlanWithdrawn {
		return domain.NewError(domain.CodeInvalidTransition, "计划 %s 已终态（%s），不能被替代", plan.Ref, plan.Status)
	}
	plan.Status = domain.PlanSuperseded
	p.graph.PutPlan(plan)
	return nil
}

func (p *Projector) applyPlanRefAction(eventType string, pl *event.PlanRefPayload) error {
	plan, err := p.requirePlan(pl.Ref)
	if err != nil {
		return err
	}
	switch eventType {
	case event.TypePlanWithdrawn:
		if plan.Status == domain.PlanSuperseded || plan.Status == domain.PlanWithdrawn {
			return domain.NewError(domain.CodeInvalidTransition, "计划 %s 已终态，不能撤回", plan.Ref)
		}
		plan.Status = domain.PlanWithdrawn
	case event.TypePlanCityConfirmed:
		if plan.Level != domain.LevelCity {
			return domain.NewError(domain.CodeCityConfirmationMissing,
				"计划 %s 层级为 %s，只有城市级计划需要城市自身确认", plan.Ref, plan.Level)
		}
		plan.CityConfirmed = true
	default:
		return domain.NewError(domain.CodeValidation, "未预期的计划事件 %s", eventType)
	}
	p.graph.PutPlan(plan)
	return nil
}

// ---- 承诺事件 ----

func commitmentFingerprint(pl *event.RegisterCommitmentPayload) string {
	return string(pl.Category) + "|" + domain.NormalizeName(pl.Title.Original)
}

// validatePlanConstraints 沿来源计划及其上级路线图链检查合作类别约束。
// 只有 binding 状态的路线图产生约束；任一 binding 祖先不允许该类别即拒绝。
func (p *Projector) validatePlanConstraints(startRefs []string, cat domain.CooperationCategory) error {
	seen := map[string]bool{}
	for _, start := range startRefs {
		if start == "" || seen[start] {
			continue
		}
		ref := start
		for ref != "" && !seen[ref] {
			seen[ref] = true
			plan, ok := p.graph.Plan(ref)
			if !ok {
				return domain.NewError(domain.CodeUnknownPlan, "约束链中的计划 %s 不存在", ref)
			}
			if plan.Status == domain.PlanBinding && !plan.AllowsCategory(cat) {
				return domain.NewError(domain.CodePlanConstraint,
					"生效路线图 %s 不允许合作类别 %s，下级计划不得安排该事项", plan.Ref, cat)
			}
			ref = plan.ParentRef
		}
	}
	return nil
}

func (p *Projector) applyRegisterCommitment(pl *event.RegisterCommitmentPayload, at time.Time) error {
	r, err := p.requireRelation(pl.RelationRef)
	if err != nil {
		return err
	}
	for party := range pl.OwnerDepts {
		if !r.HasParty(party) {
			return domain.NewError(domain.CodeReference, "责任部门归属方 %s 不是关系 %s 的一方", party, r.Ref)
		}
	}
	chain := []string{pl.SourcePlanRef, r.OriginPlanRef}
	if err := p.validatePlanConstraints(chain, pl.Category); err != nil {
		return err
	}
	dedup := commitmentFingerprint(pl)
	if existing, ok := p.graph.CommitmentByDedup(r.Ref, dedup); ok {
		return domain.NewError(domain.CodeDuplicateCommitment,
			"关系 %s 下已存在同类承诺 %s（类别=%s、标题=%q），不得重复登记",
			r.Ref, existing, pl.Category, pl.Title.Original)
	}
	var dueAt time.Time
	if pl.DueAt != "" {
		dueAt, _ = domain.ParseOccurredAt(pl.DueAt)
	}
	c := &domain.Commitment{
		Ref: pl.Ref, RelationRef: r.Ref, Category: pl.Category, Title: pl.Title,
		DedupKey: dedup, Status: domain.CommitOpen, OwnerDepts: pl.OwnerDepts,
		DueRaw: pl.DueAt, DueAt: dueAt, SourcePlanRef: pl.SourcePlanRef,
		CreatedRaw: rawTime(at), CreatedAt: at, UpdatedAt: at,
	}
	p.graph.PutCommitment(c)
	return nil
}

func (p *Projector) requireCommitment(ref string) (*domain.Commitment, error) {
	c, ok := p.graph.Commitment(ref)
	if !ok {
		return nil, domain.NewError(domain.CodeUnknownCommitment, "承诺 %s 不存在", ref)
	}
	return c, nil
}

func (p *Projector) applyProgressCommitment(pl *event.ProgressCommitmentPayload, at time.Time) error {
	c, err := p.requireCommitment(pl.Ref)
	if err != nil {
		return err
	}
	if c.Status == domain.CommitFulfilled || c.Status == domain.CommitCancelled {
		return domain.NewError(domain.CodeInvalidTransition, "承诺 %s 已终态（%s），不能更新进展", c.Ref, c.Status)
	}
	c.Status = pl.Status
	c.UpdatedRaw, c.UpdatedAt = pl.At, at
	p.graph.PutCommitment(c)
	return nil
}

func (p *Projector) applyFulfillCommitment(pl *event.FulfillCommitmentPayload, at time.Time) error {
	c, err := p.requireCommitment(pl.Ref)
	if err != nil {
		return err
	}
	if c.Status == domain.CommitCancelled {
		return domain.NewError(domain.CodeInvalidTransition, "承诺 %s 已取消，不能标记完成", c.Ref)
	}
	c.Status = domain.CommitFulfilled
	c.DoneRaw, c.DoneAt = pl.At, at
	c.UpdatedRaw, c.UpdatedAt = pl.At, at
	p.graph.PutCommitment(c)
	return nil
}

func (p *Projector) applyCancelCommitment(pl *event.CancelCommitmentPayload, at time.Time) error {
	c, err := p.requireCommitment(pl.Ref)
	if err != nil {
		return err
	}
	if c.Status == domain.CommitFulfilled {
		return domain.NewError(domain.CodeInvalidTransition, "承诺 %s 已完成，不能取消", c.Ref)
	}
	c.Status = domain.CommitCancelled
	c.UpdatedRaw, c.UpdatedAt = pl.At, at
	p.graph.PutCommitment(c)
	return nil
}

// ---- 活动事件 ----

func (p *Projector) applyRecordActivity(pl *event.RecordActivityPayload, at time.Time) error {
	r, err := p.requireRelation(pl.RelationRef)
	if err != nil {
		return err
	}
	if _, exists := p.graph.Activity(pl.Ref); exists {
		return domain.NewError(domain.CodeReference, "活动引用 %s 已存在", pl.Ref)
	}
	if pl.CommitmentRef != "" {
		c, err := p.requireCommitment(pl.CommitmentRef)
		if err != nil {
			return err
		}
		if c.RelationRef != r.Ref {
			return domain.NewError(domain.CodeReference, "承诺 %s 不属于关系 %s", pl.CommitmentRef, r.Ref)
		}
	}
	if pl.HostPartyRef != "" {
		if !r.HasParty(pl.HostPartyRef) {
			return domain.NewError(domain.CodeReference, "主办方 %s 不是关系 %s 的一方", pl.HostPartyRef, r.Ref)
		}
	}
	startAt, _ := domain.ParseOccurredAt(pl.StartAt)
	var endAt time.Time
	if pl.EndAt != "" {
		endAt, _ = domain.ParseOccurredAt(pl.EndAt)
	}
	a := &domain.Activity{
		Ref: pl.Ref, RelationRef: r.Ref, CommitmentRef: pl.CommitmentRef,
		Category: pl.Category, Title: pl.Title,
		StartRaw: pl.StartAt, StartAt: startAt, EndRaw: pl.EndAt, EndAt: endAt,
		TimeZone: pl.TimeZone, Location: pl.Location, HostPartyRef: pl.HostPartyRef,
		Outcomes: pl.Outcomes, CreatedRaw: rawTime(at), CreatedAt: at,
	}
	p.graph.PutActivity(a)
	return nil
}

// ---- 小工具 ----

func dedupCategories(in []domain.CooperationCategory) []domain.CooperationCategory {
	if len(in) == 0 {
		return nil
	}
	seen := map[domain.CooperationCategory]bool{}
	out := make([]domain.CooperationCategory, 0, len(in))
	for _, c := range in {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	return out
}

func joinReasons(rs []string) string {
	out := ""
	for i, r := range rs {
		if i > 0 {
			out += "；"
		}
		out += r
	}
	return out
}

func rawTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format(time.RFC3339)
}
