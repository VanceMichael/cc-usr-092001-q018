package service

import (
	"sort"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/store"
)

// Severity 表示发现项的阻断程度。
type Severity string

const (
	SevInfo     Severity = "info"     // 重叠/可复用提示
	SevWarning  Severity = "warning"  // 需人工注意但不绝对阻断
	SevBlocking Severity = "blocking" // 必须先处理才能继续
)

// FindingKind 对发现项分类。
type FindingKind string

const (
	KindOverlap  FindingKind = "overlap"
	KindConflict FindingKind = "conflict"
	KindReusable FindingKind = "reusable"
)

// Finding 是提议分析中的一条发现。
type Finding struct {
	Kind     FindingKind           `json:"kind"`
	Code     string                `json:"code"`
	Severity Severity              `json:"severity"`
	Message  string                `json:"message"`
	Refs     map[string]string     `json:"refs,omitempty"`
	Category domain.CooperationCategory `json:"category,omitempty"`
}

// PartyInput 用引用编号或名称指认提议一方。
type PartyInput struct {
	Ref  string `json:"ref,omitempty"`
	Name string `json:"name,omitempty"`
}

// AnalyzeProposalRequest 是一次新合作提议。
type AnalyzeProposalRequest struct {
	PartyA      PartyInput                      `json:"party_a"`
	PartyB      PartyInput                      `json:"party_b"`
	Categories  []domain.CooperationCategory    `json:"categories"`
	// 拟新增承诺标题（多语原文），用于发现重复承诺。
	Commitments []domain.LocalizedText          `json:"commitments,omitempty"`
}

// PartyResolution 是提议一方的解析结果。
type PartyResolution struct {
	Input      PartyInput       `json:"input"`
	Resolved   bool             `json:"resolved"`
	Ambiguous  bool             `json:"ambiguous"`
	CurrentRef string           `json:"current_ref,omitempty"`
	Candidates []ResolvedEntity `json:"candidates,omitempty"`
}

// ProposalAnalysis 是分析结论。
type ProposalAnalysis struct {
	PartyA         PartyResolution `json:"party_a"`
	PartyB         PartyResolution `json:"party_b"`
	Overlaps       []Finding       `json:"overlaps"`
	Conflicts      []Finding       `json:"conflicts"`
	Reusable       []Finding       `json:"reusable"`
	Recommendation string          `json:"recommendation"` // create_relation | use_existing_relation | resolve_ambiguity | blocked
}

func (r AnalyzeProposalRequest) validate() error {
	if r.PartyA.Ref == "" && r.PartyA.Name == "" {
		return domain.NewError(domain.CodeValidation, "party_a 必须提供 ref 或 name")
	}
	if r.PartyB.Ref == "" && r.PartyB.Name == "" {
		return domain.NewError(domain.CodeValidation, "party_b 必须提供 ref 或 name")
	}
	for _, c := range r.Categories {
		if !c.Valid() {
			return domain.NewError(domain.CodeValidation, "categories 含不受控类别 %q", c)
		}
	}
	return nil
}

// AnalyzeProposal 从新合作提议中发现现有网络的重叠、冲突与可复用资源。只读，不产生变更。
func (s *Service) AnalyzeProposal(req AnalyzeProposalRequest) (ProposalAnalysis, error) {
	if err := req.validate(); err != nil {
		return ProposalAnalysis{}, err
	}
	g := s.snapshot()
	g.RLock()
	defer g.RUnlock()

	out := ProposalAnalysis{
		Overlaps:  []Finding{},
		Conflicts: []Finding{},
		Reusable:  []Finding{},
	}
	out.PartyA = resolveParty(g, req.PartyA)
	out.PartyB = resolveParty(g, req.PartyB)

	addConflict := func(f Finding) { out.Conflicts = append(out.Conflicts, f) }
	addOverlap := func(f Finding) { out.Overlaps = append(out.Overlaps, f) }
	addReusable := func(f Finding) { out.Reusable = append(out.Reusable, f) }

	for _, pr := range []PartyResolution{out.PartyA, out.PartyB} {
		if pr.Ambiguous {
			addConflict(Finding{
				Kind: KindConflict, Code: "NAME_AMBIGUOUS", Severity: SevBlocking,
				Message: "名称指认到多个实体，需人工消歧后才能建立关系",
				Refs:    map[string]string{"input": pr.Input.Name},
			})
		}
	}

	aOK := out.PartyA.Resolved && !out.PartyA.Ambiguous
	bOK := out.PartyB.Resolved && !out.PartyB.Ambiguous

	// 输入显式指向已合并的旧主体时给出阻断提示：旧关系仍可追溯，但新记录须落在承接实体。
	// CurrentRef 已自动解析到承接主体，这里要求调用方改用承接引用，避免把承诺挂到历史主体。
	for _, pr := range []*PartyResolution{&out.PartyA, &out.PartyB} {
		if pr.Resolved && len(pr.Candidates) == 1 && pr.Candidates[0].MergedInto != "" &&
			pr.Candidates[0].Ref != pr.CurrentRef {
			addConflict(Finding{
				Kind: KindConflict, Code: "PARTY_MERGED", Severity: SevBlocking,
				Message: "指认的实体已在行政区调整中被合并，新合作须引用承接实体",
				Refs: map[string]string{
					"former_ref": pr.Candidates[0].Ref,
					"current_ref": pr.CurrentRef,
				},
			})
		}
	}

	var pairRef string
	var pair *domain.Relation
	if aOK && bOK {
		curA, curB := out.PartyA.CurrentRef, out.PartyB.CurrentRef
		if ref, r, ok := g.RelationByPair(curA, curB); ok {
			pairRef, pair = ref, r
			addOverlap(Finding{
				Kind: KindOverlap, Code: "EXISTING_PAIR", Severity: SevInfo,
				Message: "双方已存在友城关系",
				Refs:    map[string]string{"relation_ref": ref, "party_a": curA, "party_b": curB},
			})
			switch r.Status {
			case domain.StatusConcluded:
				addReusable(Finding{
					Kind: KindReusable, Code: "ACTIVE_RELATION", Severity: SevInfo,
					Message: "现有关系正式有效，应在既有关系下追加合作而非新建",
					Refs:    map[string]string{"relation_ref": ref},
				})
			case domain.StatusIntended:
				addConflict(Finding{
					Kind: KindConflict, Code: "RELATION_NOT_CONCLUDED", Severity: SevWarning,
					Message: "现有关系尚处意向阶段，合作前需完成法定批准、签署与城市确认",
					Refs:    map[string]string{"relation_ref": ref},
				})
			case domain.StatusSuspended:
				addConflict(Finding{
					Kind: KindConflict, Code: "RELATION_SUSPENDED", Severity: SevWarning,
					Message: "现有关系已暂停，新合作前须先恢复关系",
					Refs:    map[string]string{"relation_ref": ref},
				})
			case domain.StatusTerminated:
				addConflict(Finding{
					Kind: KindConflict, Code: "RELATION_TERMINATED", Severity: SevBlocking,
					Message: "双方关系已终止，不得在原关系下重复承诺，需先依法重新缔结",
					Refs:    map[string]string{"relation_ref": ref},
				})
			}
			// 城市确认：上级路线图不能替代城市本身确认。
			if !r.CityConfirmed && (gEntityIsCity(g, r.PartyA) || gEntityIsCity(g, r.PartyB)) {
				addConflict(Finding{
					Kind: KindConflict, Code: "CITY_NOT_CONFIRMED", Severity: SevBlocking,
					Message: "城市本身尚未确认该关系，上级路线图不能替代城市确认",
					Refs:    map[string]string{"relation_ref": ref},
				})
			}
		}

		// 共同伙伴：双方各自既有的合作对象交集，揭示网络重叠。
		neighborsA := neighborSet(g, curA)
		neighborsB := neighborSet(g, curB)
		for third := range neighborsA {
			if third == curB {
				continue
			}
			if _, ok := neighborsB[third]; ok {
				addOverlap(Finding{
					Kind: KindOverlap, Code: "COMMON_PARTNER", Severity: SevInfo,
					Message: "双方与同一第三方都存在关系，可作为对接枢纽",
					Refs:    map[string]string{"third_party": third},
				})
			}
		}
	}

	// 类别维度的约束、重复与可复用资源。
	for _, cat := range dedupCategoriesList(req.Categories) {
		if pair != nil {
			// 路线图类别约束。
			if plan := blockingPlanFor(g, pair, cat); plan != "" {
				addConflict(Finding{
					Kind: KindConflict, Code: "CATEGORY_FORBIDDEN_BY_ROADMAP", Severity: SevBlocking,
					Message: "生效上级路线图不允许该合作类别，下级计划不得安排",
					Refs:    map[string]string{"plan_ref": plan, "relation_ref": pairRef},
					Category: cat,
				})
			}
			// 重复承诺与同类既有承诺。
			for _, c := range g.CommitmentsOf(pairRef) {
				if c.Category != cat {
					continue
				}
				for _, title := range req.Commitments {
					if c.DedupKey == string(cat)+"|"+domain.NormalizeName(title.Original) &&
						c.Status != domain.CommitCancelled {
						addConflict(Finding{
							Kind: KindConflict, Code: "DUPLICATE_COMMITMENT", Severity: SevBlocking,
							Message: "同一关系下已存在相同类别与标题的承诺，重复上报不得再建",
							Refs:    map[string]string{"commitment_ref": c.Ref, "relation_ref": pairRef},
							Category: cat,
						})
					}
				}
				if c.Status != domain.CommitCancelled && c.Status != domain.CommitFulfilled {
					addReusable(Finding{
						Kind: KindReusable, Code: "ACTIVE_COMMITMENT", Severity: SevInfo,
						Message: "同类承诺仍在推进，建议接续而非另立",
						Refs:    map[string]string{"commitment_ref": c.Ref},
						Category: cat,
					})
				}
			}
			// 同类历史活动与成果。
			for _, act := range g.ActivitiesOf(pairRef) {
				if act.Category == cat {
					addReusable(Finding{
						Kind: KindReusable, Code: "PAST_ACTIVITY", Severity: SevInfo,
						Message: "该关系下已有同类活动及其成果可供复用",
						Refs:    map[string]string{"activity_ref": act.Ref, "relation_ref": pairRef},
						Category: cat,
					})
				}
			}
			// 覆盖该类别的生效计划。
			for _, p := range g.Plans() {
				if p.RelationRef == pairRef && p.Status == domain.PlanBinding && p.AllowsCategory(cat) {
					addReusable(Finding{
						Kind: KindReusable, Code: "COVERING_PLAN", Severity: SevInfo,
						Message: "已有生效计划覆盖该类别，可在其框架下安排",
						Refs:    map[string]string{"plan_ref": p.Ref},
						Category: cat,
					})
				}
			}
		}

		// 网络层面经验：双方在各自其它关系中的同类活动可复用。
		if aOK {
			collectNetworkExperience(g, out.PartyA.CurrentRef, cat, pairRef, addReusable)
		}
		if bOK {
			collectNetworkExperience(g, out.PartyB.CurrentRef, cat, pairRef, addReusable)
		}
	}

	sortFindings(out.Overlaps)
	sortFindings(out.Conflicts)
	sortFindings(out.Reusable)
	out.Recommendation = recommend(out, pair)
	return out, nil
}

func recommend(out ProposalAnalysis, pair *domain.Relation) string {
	if out.PartyA.Ambiguous || out.PartyB.Ambiguous {
		return "resolve_ambiguity"
	}
	for _, f := range out.Conflicts {
		if f.Severity == SevBlocking {
			return "blocked"
		}
	}
	if pair != nil && pair.Status == domain.StatusConcluded {
		return "use_existing_relation"
	}
	return "create_relation"
}

// resolveParty 按 ref（优先）或名称解析一方，含同名歧义与合并承接。
func resolveParty(g *store.Graph, in PartyInput) PartyResolution {
	pr := PartyResolution{Input: in}
	if in.Ref != "" {
		e := g.ResolveEntity(in.Ref)
		if e == nil {
			return pr
		}
		pr.Resolved = true
		pr.CurrentRef = e.Ref
		pr.Candidates = []ResolvedEntity{{
			Ref: in.Ref, Level: e.Level, CurrentName: e.CurrentName,
			MatchedOn: "ref", MergedInto: entityMergedInto(g, in.Ref), CurrentRef: e.Ref,
		}}
		return pr
	}
	cands := []ResolvedEntity{}
	for _, ref := range g.FindByName(in.Name) {
		e, ok := g.Entity(ref)
		if !ok {
			continue
		}
		matchedOn := "historical"
		if domain.NormalizeName(e.CurrentName) == domain.NormalizeName(in.Name) {
			matchedOn = "current"
		}
		currentRef := ref
		if cur := g.ResolveEntity(ref); cur != nil {
			currentRef = cur.Ref
		}
		cands = append(cands, ResolvedEntity{
			Ref: ref, Level: e.Level, CurrentName: e.CurrentName,
			MatchedOn: matchedOn, MergedInto: e.MergedInto, CurrentRef: currentRef,
		})
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Ref < cands[j].Ref })
	pr.Candidates = cands
	switch len(cands) {
	case 0:
		return pr
	case 1:
		pr.Resolved = true
		pr.CurrentRef = cands[0].CurrentRef
	default:
		pr.Ambiguous = true
		pr.CurrentRef = ""
	}
	return pr
}

func entityMergedInto(g *store.Graph, ref string) string {
	if e, ok := g.Entity(ref); ok {
		return e.MergedInto
	}
	return ""
}

func gEntityIsCity(g *store.Graph, ref string) bool {
	if e := g.ResolveEntity(ref); e != nil {
		return e.Level == domain.LevelCity
	}
	return false
}

func neighborSet(g *store.Graph, ref string) map[string]struct{} {
	set := map[string]struct{}{}
	for _, r := range g.RelationsOf(ref) {
		other := r.OtherParty(ref)
		if cur := g.ResolveEntity(other); cur != nil {
			other = cur.Ref
		}
		set[other] = struct{}{}
	}
	return set
}

// blockingPlanFor 沿关系来源计划与其上级链查找禁止该类别的生效路线图。
func blockingPlanFor(g *store.Graph, r *domain.Relation, cat domain.CooperationCategory) string {
	seen := map[string]bool{}
	ref := r.OriginPlanRef
	for ref != "" && !seen[ref] {
		seen[ref] = true
		plan, ok := g.Plan(ref)
		if !ok {
			return ""
		}
		if plan.Status == domain.PlanBinding && !plan.AllowsCategory(cat) {
			return ref
		}
		ref = plan.ParentRef
	}
	return ""
}

func collectNetworkExperience(g *store.Graph, party string, cat domain.CooperationCategory, excludeRelation string, add func(Finding)) {
	seen := map[string]bool{}
	for _, r := range g.RelationsOf(party) {
		if r.Ref == excludeRelation {
			continue
		}
		for _, act := range g.ActivitiesOf(r.Ref) {
			if act.Category != cat || seen[act.Ref] {
				continue
			}
			seen[act.Ref] = true
			add(Finding{
				Kind: KindReusable, Code: "NETWORK_EXPERIENCE", Severity: SevInfo,
				Message: "该方在既有网络中有同类活动经验可借鉴",
				Refs:    map[string]string{"activity_ref": act.Ref, "relation_ref": r.Ref},
				Category: cat,
			})
		}
	}
}

func dedupCategoriesList(in []domain.CooperationCategory) []domain.CooperationCategory {
	seen := map[domain.CooperationCategory]bool{}
	out := make([]domain.CooperationCategory, 0, len(in))
	for _, c := range in {
		if !seen[c] {
			seen[c] = true
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		if fs[i].Code != fs[j].Code {
			return fs[i].Code < fs[j].Code
		}
		return fs[i].Message < fs[j].Message
	})
}
