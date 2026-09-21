package graph

import (
	"sort"
	"time"

	"example.com/batch-092001-q018/internal/domain"
)

// EntityDossier 汇总实体本身、名称历史、合并继受与相关关系。
type EntityDossier struct {
	Entity         EntityView     `json:"entity"`
	CurrentRef     string         `json:"current_ref"`
	Predecessors   []string       `json:"predecessors,omitempty"`    // 并入本实体的旧实体
	SuccessorChain []string       `json:"successor_chain,omitempty"` // 本实体被并入时的继受链
	Relations      []RelationView `json:"relations,omitempty"`       // 含本实体或其前身的全部关系
	NameMatches    []string       `json:"name_matches,omitempty"`    // 同名实体（名称冲突侦测）
}

// PlanConstraint 是卷宗中的计划约束视图，明确区分挂接与城市确认。
type PlanConstraint struct {
	Plan              *PlanView `json:"plan"`
	BoundAt           string    `json:"bound_at"`
	ConfirmedByCities []string  `json:"confirmed_by_cities"`
	PendingCities     []string  `json:"pending_cities"`
}

// RelationDossier 是沿任一友城对查看的完整卷宗。
type RelationDossier struct {
	Relation             RelationView     `json:"relation"`
	ApprovalsComplete    bool             `json:"approvals_complete"`
	Plans                []PlanConstraint `json:"plans"`
	Commitments          []CommitmentView `json:"commitments"`
	OpenCommitments      []CommitmentView `json:"open_commitments"`
	OverdueCommitments   []CommitmentView `json:"overdue_commitments"`
	FulfilledCommitments []CommitmentView `json:"fulfilled_commitments"`
	Activities           []ActivityView   `json:"activities"`
	ResponsibleDepts     []string         `json:"responsible_departments"`
}

// ProposalIssue 描述新合作提议与现有网络的冲突或重叠。
type ProposalIssue struct {
	Kind        string `json:"kind"`     // duplicate_relation / name_collision / same_name_side / merge_chain
	Severity    string `json:"severity"` // conflict / info
	Message     string `json:"message"`
	RelationRef string `json:"relation_ref,omitempty"`
	EntityRef   string `json:"entity_ref,omitempty"`
}

// ReusableResource 是可复用的既有资源（已有承诺/活动的类别）。
type ReusableResource struct {
	Category    string `json:"category"`
	Source      string `json:"source"` // commitment / activity
	Ref         string `json:"ref"`
	RelationRef string `json:"relation_ref"`
}

// ProposalAnalysis 是对新合作提议的分析结果。
type ProposalAnalysis struct {
	EntityA           string                `json:"entity_a"`
	EntityB           string                `json:"entity_b"`
	ResolvedA         string                `json:"resolved_a"`
	ResolvedB         string                `json:"resolved_b"`
	ExistingRelation  string                `json:"existing_relation,omitempty"`
	ExistingStatus    domain.RelationStatus `json:"existing_status,omitempty"`
	Issues            []ProposalIssue       `json:"issues"`
	ReusableResources []ReusableResource    `json:"reusable_resources"`
	CategoriesCovered []string              `json:"categories_covered"`
}

// Entity 返回实体视图副本，不存在时第二个返回值为 false。
func (g *Graph) Entity(ref string) (EntityView, bool) {
	ent, ok := g.entities[ref]
	if !ok {
		return EntityView{}, false
	}
	view := ent.view
	view.MergedInto = ent.mergedInto
	view.MergedAt = ent.mergedAt
	return view, true
}

// relationsTouched 返回以 ref 或其前身为缔约方的关系（按引用排序）。
func (g *Graph) relationsTouched(ref string) []RelationView {
	want := map[string]bool{ref: true}
	for _, p := range g.predecessors(ref) {
		want[p] = true
	}
	var out []RelationView
	for relRef, r := range g.relations {
		if want[r.view.EntityA] || want[r.view.EntityB] {
			out = append(out, g.snapshotRelation(relRef))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

// DossierEntity 返回实体卷宗：名称、继受、关系与同名冲突。
func (g *Graph) DossierEntity(ref string) (EntityDossier, bool) {
	ent, ok := g.entities[ref]
	if !ok {
		return EntityDossier{}, false
	}
	view := ent.view
	dossier := EntityDossier{
		Entity:       view,
		CurrentRef:   g.Resolve(ref),
		Predecessors: g.predecessors(ref),
		Relations:    g.relationsTouched(ref),
	}
	// 继受链：若实体已被并入，逐跳给出去向。
	for cur := ref; g.entities[cur] != nil && g.entities[cur].mergedInto != ""; {
		cur = g.entities[cur].mergedInto
		dossier.SuccessorChain = append(dossier.SuccessorChain, cur)
	}
	// 同名侦测：任何语言版本与其他实体撞名即列出。
	matches := map[string]bool{}
	for _, nr := range view.Names {
		for lang, value := range nr.Name {
			for other := range g.names[lang+"|"+lowerTrim(value)] {
				if other != ref && !matches[other] {
					matches[other] = true
					dossier.NameMatches = append(dossier.NameMatches, other)
				}
			}
		}
	}
	sort.Strings(dossier.NameMatches)
	return dossier, true
}

// Relation 返回关系视图副本。
func (g *Graph) Relation(ref string) (RelationView, bool) {
	if _, ok := g.relations[ref]; !ok {
		return RelationView{}, false
	}
	return g.snapshotRelation(ref), true
}

func (g *Graph) snapshotRelation(ref string) RelationView {
	r := g.relations[ref]
	view := r.view
	view.Approvals = append([]ApprovalRecord(nil), r.view.Approvals...)
	view.Texts = append([]TextRecord(nil), r.view.Texts...)
	view.History = append([]StatusChange(nil), r.view.History...)
	return view
}

// DossierRelation 沿友城对汇总承诺、活动成果、逾期事项、计划约束与责任部门。
// now 为查询时刻（UTC 绝对瞬时），跨时区活动与日期截止均按绝对时刻比较。
func (g *Graph) DossierRelation(ref string, now time.Time) (RelationDossier, bool) {
	r, ok := g.relations[ref]
	if !ok {
		return RelationDossier{}, false
	}
	dossier := RelationDossier{Relation: g.snapshotRelation(ref)}
	dossier.ApprovalsComplete = g.bothSidesApproved(r)

	depts := map[string]bool{}
	if r.view.ResponsibleDept != "" {
		depts[r.view.ResponsibleDept] = true
	}

	for planRef := range r.plans {
		pc := PlanConstraint{Plan: g.plans[planRef]}
		conf := g.plans[planRef].RelationConfirmations[ref]
		pc.BoundAt = conf.BoundAt
		for city, at := range conf.ConfirmedByCities {
			pc.ConfirmedByCities = append(pc.ConfirmedByCities, city)
			_ = at
		}
		for _, side := range []string{r.view.EntityA, r.view.EntityB} {
			if _, yes := conf.ConfirmedByCities[side]; !yes {
				pc.PendingCities = append(pc.PendingCities, side)
			}
		}
		sort.Strings(pc.ConfirmedByCities)
		sort.Strings(pc.PendingCities)
		dossier.Plans = append(dossier.Plans, pc)
	}
	sort.Slice(dossier.Plans, func(i, j int) bool { return dossier.Plans[i].Plan.Ref < dossier.Plans[j].Plan.Ref })

	for cRef := range r.commitments {
		c := *g.commitments[cRef]
		dossier.Commitments = append(dossier.Commitments, c)
		depts[c.ResponsibleDept] = true
		switch {
		case c.Fulfilled:
			dossier.FulfilledCommitments = append(dossier.FulfilledCommitments, c)
		case g.isOverdue(c, now):
			dossier.OverdueCommitments = append(dossier.OverdueCommitments, c)
		default:
			dossier.OpenCommitments = append(dossier.OpenCommitments, c)
		}
	}
	for aRef := range r.activities {
		a := *g.activities[aRef]
		dossier.Activities = append(dossier.Activities, a)
		if a.ResponsibleDept != "" {
			depts[a.ResponsibleDept] = true
		}
	}
	less := func(i, j int) bool { return dossier.Commitments[i].Ref < dossier.Commitments[j].Ref }
	for _, slice := range [][]CommitmentView{dossier.Commitments, dossier.OpenCommitments, dossier.OverdueCommitments, dossier.FulfilledCommitments} {
		sort.Slice(slice, less)
	}
	sort.Slice(dossier.Activities, func(i, j int) bool { return dossier.Activities[i].Ref < dossier.Activities[j].Ref })

	for dept := range depts {
		dossier.ResponsibleDepts = append(dossier.ResponsibleDepts, dept)
	}
	sort.Strings(dossier.ResponsibleDepts)
	return dossier, true
}

// isOverdue 比较承诺截止日当天结束（UTC）与查询时刻。
func (g *Graph) isOverdue(c CommitmentView, now time.Time) bool {
	deadline, err := domain.ParseDate(c.DueDate, "截止日期")
	if err != nil {
		return false // 事件入库时已校验，防御性处理
	}
	return now.After(deadline)
}

// AnalyzeProposal 分析在 entityA/entityB 之间开展新合作的可行性：
//   - 解析合并继受，识别既有存续关系（重复上报的冲突）；
//   - 侦测同名实体可能造成的混淆；
//   - 汇总两侧现有网络中按类别可复用的承诺与活动资源。
func (g *Graph) AnalyzeProposal(entityA, entityB string) (ProposalAnalysis, error) {
	if _, ok := g.entities[entityA]; !ok {
		return ProposalAnalysis{}, missingEntityError(entityA)
	}
	if _, ok := g.entities[entityB]; !ok {
		return ProposalAnalysis{}, missingEntityError(entityB)
	}
	if entityA == entityB {
		return ProposalAnalysis{}, errSameEntity
	}
	resolvedA, resolvedB := g.Resolve(entityA), g.Resolve(entityB)
	result := ProposalAnalysis{
		EntityA: entityA, EntityB: entityB,
		ResolvedA: resolvedA, ResolvedB: resolvedB,
	}
	if resolvedA == resolvedB {
		result.Issues = append(result.Issues, ProposalIssue{
			Kind: "merge_chain", Severity: "conflict",
			Message: "两个实体经合并继受后指向同一现行实体，不能缔结双边关系",
		})
	}

	// 既有关系：解析继受后的实体对上的全部关系。
	key := domain.PairKey(resolvedA, resolvedB)
	for _, relRef := range g.pairs[key] {
		r := g.relations[relRef]
		if live(r.view.Status) {
			result.ExistingRelation = relRef
			result.ExistingStatus = r.view.Status
			result.Issues = append(result.Issues, ProposalIssue{
				Kind: "duplicate_relation", Severity: "conflict",
				Message:     "实体对（含合并继受）已有存续关系，重复上报不得创建第二对关系",
				RelationRef: relRef,
			})
		}
	}

	// 同名冲突：两侧名称若在任一语言下撞向不同实体，提示可能的城市同名混淆。
	flagNameCollisions := func(self, other string) {
		ent := g.entities[self]
		others := map[string]bool{}
		for _, nr := range ent.view.Names {
			for lang, value := range nr.Name {
				for match := range g.names[lang+"|"+lowerTrim(value)] {
					if match != self && g.Resolve(match) != g.Resolve(other) && match != other {
						others[match] = true
					}
				}
			}
		}
		for match := range others {
			result.Issues = append(result.Issues, ProposalIssue{
				Kind: "same_name_side", Severity: "info",
				Message:   "提议方存在同名的其他行政实体，请核对稳定标识是否指向正确城市",
				EntityRef: match,
			})
		}
	}
	flagNameCollisions(entityA, entityB)
	flagNameCollisions(entityB, entityA)

	// 可复用资源：两侧现有网络中的承诺与活动，按类别汇总。
	sideRefs := func(start string) map[string]bool {
		set := map[string]bool{start: true}
		for _, p := range g.predecessors(start) {
			set[p] = true
		}
		return set
	}
	aSide, bSide := sideRefs(resolvedA), sideRefs(resolvedB)
	categories := map[string]bool{}
	for _, r := range g.relations {
		if !(aSide[r.view.EntityA] || aSide[r.view.EntityB]) &&
			!(bSide[r.view.EntityA] || bSide[r.view.EntityB]) {
			continue
		}
		// 只统计两侧各自现有网络，排除提议双方之间既有的（可能已终止的）关系。
		if (aSide[r.view.EntityA] && bSide[r.view.EntityB]) || (aSide[r.view.EntityB] && bSide[r.view.EntityA]) {
			continue
		}
		for cRef := range r.commitments {
			c := g.commitments[cRef]
			if c.Fulfilled {
				// 已兑现承诺不再是可对接的开放资源；其成果以活动形式体现。
				continue
			}
			result.ReusableResources = append(result.ReusableResources, ReusableResource{
				Category: c.Category, Source: "commitment", Ref: c.Ref, RelationRef: c.RelationRef,
			})
			categories[c.Category] = true
		}
		for aRef := range r.activities {
			a := g.activities[aRef]
			result.ReusableResources = append(result.ReusableResources, ReusableResource{
				Category: a.Category, Source: "activity", Ref: a.Ref, RelationRef: a.RelationRef,
			})
			categories[a.Category] = true
		}
	}
	sort.Slice(result.ReusableResources, func(i, j int) bool {
		if result.ReusableResources[i].Ref != result.ReusableResources[j].Ref {
			return result.ReusableResources[i].Ref < result.ReusableResources[j].Ref
		}
		return result.ReusableResources[i].Source < result.ReusableResources[j].Source
	})
	for cat := range categories {
		result.CategoriesCovered = append(result.CategoriesCovered, cat)
	}
	sort.Strings(result.CategoriesCovered)
	sortIssues(result.Issues)
	return result, nil
}
