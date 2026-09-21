// Package graph 是事件日志的读模型投影。所有当前状态都由事件重放得到：
// 名称历史、合并继受链、关系状态机、双边批准、文本版本、计划约束与
// 城市确认、承诺与活动成果均在此汇总，并提供友城卷宗与新提议分析。
package graph

import (
	"fmt"
	"sort"
	"strings"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/events"
)

// NameRecord 保存实体的一个名称及其启用日期。
type NameRecord struct {
	NameRef    string               `json:"name_ref"`
	Name       domain.LocalizedText `json:"name"`
	ValidFrom  string               `json:"valid_from"`
	Historical bool                 `json:"historical"`
}

// EntityView 是行政实体的稳定标识视图。
type EntityView struct {
	Ref        string             `json:"ref"`
	Level      domain.EntityLevel `json:"level"`
	ParentRef  string             `json:"parent_ref,omitempty"`
	Names      []NameRecord       `json:"names"`
	MergedInto string             `json:"merged_into,omitempty"`
	MergedAt   string             `json:"merged_at,omitempty"`
}

// StatusChange 记录一次关系状态变迁，保证暂停、终止等过程可追溯。
type StatusChange struct {
	Status     domain.RelationStatus `json:"status"`
	OccurredAt string                `json:"occurred_at"`
	Reason     string                `json:"reason,omitempty"`
}

// ApprovalRecord 保存一次法定批准记录。
type ApprovalRecord struct {
	Ref              string                  `json:"ref"`
	Side             domain.ApprovalSide     `json:"side"`
	Decision         domain.ApprovalDecision `json:"decision"`
	Authority        string                  `json:"authority"`
	DecidedAt        string                  `json:"decided_at"`
	InstrumentDigest string                  `json:"instrument_digest,omitempty"`
}

// TextRecord 保存关系文本的一个版本。
type TextRecord struct {
	Ref      string          `json:"ref"`
	Kind     domain.TextKind `json:"kind"`
	Language string          `json:"language"`
	Content  string          `json:"content"`
	Version  int             `json:"version"`
	Signed   bool            `json:"signed"`
	SignedAt string          `json:"signed_at,omitempty"`
}

// PlanView 是路线图或交往计划的视图。
type PlanView struct {
	Ref         string               `json:"ref"`
	Tier        domain.PlanTier      `json:"tier"`
	EntityA     string               `json:"entity_a"`
	EntityB     string               `json:"entity_b"`
	ParentRef   string               `json:"parent_ref,omitempty"`
	Title       domain.LocalizedText `json:"title"`
	AdoptedAt   string               `json:"adopted_at"`
	PeriodStart string               `json:"period_start,omitempty"`
	PeriodEnd   string               `json:"period_end,omitempty"`
	// RelationConfirmations 以关系引用为键，记录城市确认状态。
	RelationConfirmations map[string]Confirmation `json:"relation_confirmations,omitempty"`
}

// Confirmation 区分“上级文件已挂接”与“城市本身已确认”。
type Confirmation struct {
	BoundAt           string            `json:"bound_at"`
	ConfirmedByCities map[string]string `json:"confirmed_by_cities,omitempty"` // 城市引用 -> 确认时间
}

// CommitmentView 是承诺及其履行状态。
type CommitmentView struct {
	Ref               string               `json:"ref"`
	RelationRef       string               `json:"relation_ref"`
	Category          string               `json:"category"`
	Description       domain.LocalizedText `json:"description"`
	ResponsibleDept   string               `json:"responsible_dept"`
	DueDate           string               `json:"due_date"`
	PlanRef           string               `json:"plan_ref,omitempty"`
	Fulfilled         bool                 `json:"fulfilled"`
	FulfilledAt       string               `json:"fulfilled_at,omitempty"`
	FulfillmentActRef string               `json:"fulfillment_activity_ref,omitempty"`
}

// ActivityView 是跨时区活动及其成果。
type ActivityView struct {
	Ref             string               `json:"ref"`
	RelationRef     string               `json:"relation_ref"`
	Category        string               `json:"category"`
	Title           domain.LocalizedText `json:"title"`
	StartAt         string               `json:"start_at"`
	EndAt           string               `json:"end_at,omitempty"`
	Timezone        string               `json:"timezone,omitempty"`
	Location        string               `json:"location,omitempty"`
	Outcomes        domain.LocalizedText `json:"outcomes,omitempty"`
	CommitmentRefs  []string             `json:"commitment_refs,omitempty"`
	ResponsibleDept string               `json:"responsible_dept,omitempty"`
}

// RelationView 是一对友城关系的汇总视图。
type RelationView struct {
	Ref             string                `json:"ref"`
	EntityA         string                `json:"entity_a"`
	EntityB         string                `json:"entity_b"`
	Status          domain.RelationStatus `json:"status"`
	ResponsibleDept string                `json:"responsible_dept,omitempty"`
	Title           domain.LocalizedText  `json:"title,omitempty"`
	SigningDate     string                `json:"signing_date,omitempty"`
	EffectiveDate   string                `json:"effective_date,omitempty"`
	History         []StatusChange        `json:"history"`
	Approvals       []ApprovalRecord      `json:"approvals"`
	Texts           []TextRecord          `json:"texts"`
}

type entity struct {
	view       EntityView
	mergedInto string
	mergedAt   string
}

type relation struct {
	view        RelationView
	approvals   map[domain.ApprovalSide]ApprovalRecord // 每侧最新决定
	textKeys    map[string]bool                        // 语言|类型|版本 去重
	plans       map[string]bool                        // 挂接到本关系的计划
	commitments map[string]bool
	activities  map[string]bool
}

// Graph 是内存投影；调用方（service）负责加写锁。
type Graph struct {
	entities    map[string]*entity
	names       map[string]map[string]struct{} // 语言|小写名称 -> 实体集合
	relations   map[string]*relation
	pairs       map[string][]string // 规范化实体对 -> 全部关系（含终止）
	plans       map[string]*PlanView
	commitments map[string]*CommitmentView
	activities  map[string]*ActivityView
}

// New 创建空投影。
func New() *Graph {
	return &Graph{
		entities:    make(map[string]*entity),
		names:       make(map[string]map[string]struct{}),
		relations:   make(map[string]*relation),
		pairs:       make(map[string][]string),
		plans:       make(map[string]*PlanView),
		commitments: make(map[string]*CommitmentView),
		activities:  make(map[string]*ActivityView),
	}
}

// Apply 使投影吸收一条事件。先做状态校验再变更，校验失败时投影不变。
func (g *Graph) Apply(p events.Payload, occurredAt string) error {
	if err := g.Validate(p); err != nil {
		return err
	}
	switch e := p.(type) {
	case events.EntityRegistered:
		g.entities[e.Ref] = &entity{view: EntityView{
			Ref:       e.Ref,
			Level:     e.Level,
			ParentRef: e.ParentRef,
			Names:     []NameRecord{{Name: e.Names, ValidFrom: "1970-01-01"}},
		}}
		g.indexNames(e.Ref, e.Names)
	case events.NameAttached:
		ent := g.entities[e.EntityRef]
		ent.view.Names = append(ent.view.Names, NameRecord{
			NameRef:    e.NameRef,
			Name:       e.Name,
			ValidFrom:  e.ValidFrom,
			Historical: e.Historical,
		})
		g.indexNames(e.EntityRef, e.Name)
	case events.EntityMerged:
		g.entities[e.FromRef].mergedInto = e.IntoRef
		g.entities[e.FromRef].mergedAt = e.EffectiveDate
		g.entities[e.FromRef].view.MergedInto = e.IntoRef
		g.entities[e.FromRef].view.MergedAt = e.EffectiveDate
		// 继受改变了现行实体对，按解析后的引用重建配对索引，
		// 使跨合并的重复配对仍能被识别。
		g.reindexPairs()
	case events.RelationProposed:
		a, b := domain.CanonicalPair(e.EntityA, e.EntityB)
		rel := &relation{
			view: RelationView{
				Ref:             e.Ref,
				EntityA:         a,
				EntityB:         b,
				Status:          domain.StatusIntended,
				ResponsibleDept: e.ResponsibleDept,
				Title:           e.Title,
				History: []StatusChange{
					{Status: domain.StatusIntended, OccurredAt: occurredAt},
				},
			},
			approvals:   make(map[domain.ApprovalSide]ApprovalRecord),
			textKeys:    make(map[string]bool),
			plans:       make(map[string]bool),
			commitments: make(map[string]bool),
			activities:  make(map[string]bool),
		}
		g.relations[e.Ref] = rel
		key := g.resolvedPairKey(a, b)
		g.pairs[key] = append(g.pairs[key], e.Ref)
	case events.RelationConcluded:
		r := g.relations[e.Ref]
		r.view.Status = domain.StatusConcluded
		r.view.SigningDate = e.SigningDate
		r.view.EffectiveDate = e.EffectiveDate
		r.view.History = append(r.view.History, StatusChange{
			Status: domain.StatusConcluded, OccurredAt: occurredAt,
		})
	case events.RelationSuspended:
		r := g.relations[e.Ref]
		r.view.Status = domain.StatusSuspended
		r.view.History = append(r.view.History, StatusChange{
			Status: domain.StatusSuspended, OccurredAt: occurredAt, Reason: e.Reason,
		})
	case events.RelationResumed:
		r := g.relations[e.Ref]
		r.view.Status = domain.StatusConcluded
		r.view.History = append(r.view.History, StatusChange{
			Status: domain.StatusConcluded, OccurredAt: occurredAt,
		})
	case events.RelationTerminated:
		r := g.relations[e.Ref]
		r.view.Status = domain.StatusTerminated
		r.view.History = append(r.view.History, StatusChange{
			Status: domain.StatusTerminated, OccurredAt: occurredAt, Reason: e.Reason,
		})
	case events.ApprovalRecorded:
		r := g.relations[e.RelationRef]
		rec := ApprovalRecord{
			Ref: e.Ref, Side: e.Side, Decision: e.Decision, Authority: e.Authority,
			DecidedAt: e.DecidedAt, InstrumentDigest: e.InstrumentDigest,
		}
		r.approvals[e.Side] = rec
		r.view.Approvals = append(r.view.Approvals, rec)
	case events.TextRecorded:
		r := g.relations[e.RelationRef]
		r.textKeys[textKey(e.Language, e.Kind, e.Version)] = true
		r.view.Texts = append(r.view.Texts, TextRecord{
			Ref: e.Ref, Kind: e.Kind, Language: e.Language, Content: e.Content,
			Version: e.Version, Signed: e.SignedAt != "", SignedAt: e.SignedAt,
		})
	case events.PlanAdopted:
		g.plans[e.Ref] = &PlanView{
			Ref:                   e.Ref,
			Tier:                  e.Tier,
			EntityA:               e.EntityA,
			EntityB:               e.EntityB,
			ParentRef:             e.ParentRef,
			Title:                 e.Title,
			AdoptedAt:             e.AdoptedAt,
			PeriodStart:           e.PeriodStart,
			PeriodEnd:             e.PeriodEnd,
			RelationConfirmations: make(map[string]Confirmation),
		}
	case events.PlanBoundToRelation:
		plan := g.plans[e.PlanRef]
		conf := plan.RelationConfirmations[e.RelationRef]
		if conf.ConfirmedByCities == nil {
			conf.ConfirmedByCities = make(map[string]string)
		}
		if conf.BoundAt == "" {
			conf.BoundAt = e.At
		}
		plan.RelationConfirmations[e.RelationRef] = conf
		g.relations[e.RelationRef].plans[e.PlanRef] = true
	case events.CityPlanConfirmed:
		plan := g.plans[e.PlanRef]
		conf := plan.RelationConfirmations[e.RelationRef]
		if conf.ConfirmedByCities == nil {
			conf.ConfirmedByCities = make(map[string]string)
		}
		conf.ConfirmedByCities[e.CityRef] = e.At
		plan.RelationConfirmations[e.RelationRef] = conf
	case events.CommitmentLogged:
		c := &CommitmentView{
			Ref: e.Ref, RelationRef: e.RelationRef, Category: e.Category,
			Description: e.Description, ResponsibleDept: e.ResponsibleDept,
			DueDate: e.DueDate, PlanRef: e.PlanRef,
		}
		g.commitments[e.Ref] = c
		g.relations[e.RelationRef].commitments[e.Ref] = true
	case events.CommitmentFulfilled:
		c := g.commitments[e.Ref]
		c.Fulfilled = true
		c.FulfilledAt = e.FulfilledAt
		c.FulfillmentActRef = e.ActivityRef
	case events.ActivityHeld:
		a := &ActivityView{
			Ref: e.Ref, RelationRef: e.RelationRef, Category: e.Category,
			Title: e.Title, StartAt: e.StartAt, EndAt: e.EndAt, Timezone: e.Timezone,
			Location: e.Location, Outcomes: e.Outcomes, CommitmentRefs: e.CommitmentRefs,
			ResponsibleDept: e.ResponsibleDept,
		}
		g.activities[e.Ref] = a
		g.relations[e.RelationRef].activities[e.Ref] = true
	default:
		return fmt.Errorf("投影不认识的事件类型 %T", p)
	}
	return nil
}

func textKey(lang string, kind domain.TextKind, version int) string {
	return fmt.Sprintf("%s|%s|%d", lang, kind, version)
}

func (g *Graph) indexNames(entityRef string, text domain.LocalizedText) {
	for lang, value := range text {
		key := lang + "|" + strings.ToLower(strings.TrimSpace(value))
		if g.names[key] == nil {
			g.names[key] = make(map[string]struct{})
		}
		g.names[key][entityRef] = struct{}{}
	}
}

// reindexPairs 按继受解析后的现行实体重建无序对索引。
func (g *Graph) reindexPairs() {
	pairs := make(map[string][]string, len(g.pairs))
	var refs []string
	for ref := range g.relations {
		refs = append(refs, ref)
	}
	sort.Strings(refs)
	for _, ref := range refs {
		r := g.relations[ref]
		key := g.resolvedPairKey(r.view.EntityA, r.view.EntityB)
		pairs[key] = append(pairs[key], ref)
	}
	g.pairs = pairs
}

// live 报告关系是否处于未终止状态。
func live(status domain.RelationStatus) bool {
	return status != domain.StatusTerminated
}

// resolvedPairKey 以继受后的实体引用计算无序对键，
// 使合并前后的重复配对仍能被识别。
func (g *Graph) resolvedPairKey(a, b string) string {
	return domain.PairKey(g.Resolve(a), g.Resolve(b))
}

// Validate 检查事件对当前投影状态是否可应用（形状自检在载荷层完成）。
func (g *Graph) Validate(p events.Payload) error {
	switch e := p.(type) {
	case events.EntityRegistered:
		if _, ok := g.entities[e.Ref]; ok {
			return fmt.Errorf("实体标识已存在: %s", e.Ref)
		}
		if e.ParentRef != "" {
			if _, ok := g.entities[e.ParentRef]; !ok {
				return fmt.Errorf("上级实体不存在: %s", e.ParentRef)
			}
		}
	case events.NameAttached:
		if _, ok := g.entities[e.EntityRef]; !ok {
			return fmt.Errorf("名称必须挂接到已登记实体: %s", e.EntityRef)
		}
	case events.EntityMerged:
		from, ok := g.entities[e.FromRef]
		if !ok {
			return fmt.Errorf("被合并实体不存在: %s", e.FromRef)
		}
		if from.mergedInto != "" {
			return fmt.Errorf("实体 %s 已并入 %s，不能再次合并", e.FromRef, from.mergedInto)
		}
		if _, ok := g.entities[e.IntoRef]; !ok {
			return fmt.Errorf("并入目标实体不存在: %s", e.IntoRef)
		}
	case events.RelationProposed:
		if _, ok := g.relations[e.Ref]; ok {
			return fmt.Errorf("关系标识已存在: %s", e.Ref)
		}
		if _, ok := g.entities[e.EntityA]; !ok {
			return fmt.Errorf("关系侧实体不存在: %s", e.EntityA)
		}
		if _, ok := g.entities[e.EntityB]; !ok {
			return fmt.Errorf("关系侧实体不存在: %s", e.EntityB)
		}
		for _, ref := range g.pairs[g.resolvedPairKey(e.EntityA, e.EntityB)] {
			if live(g.relations[ref].view.Status) {
				return fmt.Errorf("实体对已有存续关系 %s，重复上报不得创建第二对关系", ref)
			}
		}
	case events.RelationConcluded:
		r, ok := g.relations[e.Ref]
		if !ok {
			return fmt.Errorf("关系不存在: %s", e.Ref)
		}
		if r.view.Status != domain.StatusIntended {
			return fmt.Errorf("只有意向关系可以正式缔结，当前状态: %s", r.view.Status)
		}
		if !g.bothSidesApproved(r) {
			return fmt.Errorf("双方法定批准未齐备，不得正式缔结")
		}
	case events.RelationSuspended:
		r, ok := g.relations[e.Ref]
		if !ok {
			return fmt.Errorf("关系不存在: %s", e.Ref)
		}
		if r.view.Status != domain.StatusConcluded {
			return fmt.Errorf("只有正式缔结关系可以暂停，当前状态: %s", r.view.Status)
		}
	case events.RelationResumed:
		r, ok := g.relations[e.Ref]
		if !ok {
			return fmt.Errorf("关系不存在: %s", e.Ref)
		}
		if r.view.Status != domain.StatusSuspended {
			return fmt.Errorf("只有暂停关系可以恢复，当前状态: %s", r.view.Status)
		}
	case events.RelationTerminated:
		r, ok := g.relations[e.Ref]
		if !ok {
			return fmt.Errorf("关系不存在: %s", e.Ref)
		}
		if !live(r.view.Status) {
			return fmt.Errorf("关系已终止，不能重复终止")
		}
	case events.ApprovalRecorded:
		r, ok := g.relations[e.RelationRef]
		if !ok {
			return fmt.Errorf("批准记录对应的关系不存在: %s", e.RelationRef)
		}
		if !live(r.view.Status) {
			return fmt.Errorf("关系已终止，不再接受批准记录")
		}
	case events.TextRecorded:
		r, ok := g.relations[e.RelationRef]
		if !ok {
			return fmt.Errorf("文本对应的关系不存在: %s", e.RelationRef)
		}
		key := textKey(e.Language, e.Kind, e.Version)
		if r.textKeys[key] {
			return fmt.Errorf("文本版本已存在（语言 %s, 类型 %s, 版本 %d）", e.Language, e.Kind, e.Version)
		}
	case events.PlanAdopted:
		if _, ok := g.plans[e.Ref]; ok {
			return fmt.Errorf("计划标识已存在: %s", e.Ref)
		}
		if _, ok := g.entities[e.EntityA]; !ok {
			return fmt.Errorf("计划侧实体不存在: %s", e.EntityA)
		}
		if _, ok := g.entities[e.EntityB]; !ok {
			return fmt.Errorf("计划侧实体不存在: %s", e.EntityB)
		}
		if e.ParentRef != "" {
			parent, ok := g.plans[e.ParentRef]
			if !ok {
				return fmt.Errorf("上级路线图不存在: %s", e.ParentRef)
			}
			if parent.Tier != domain.PlanRoadmap {
				return fmt.Errorf("计划的上级必须是路线图: %s", e.ParentRef)
			}
		}
	case events.PlanBoundToRelation:
		plan, ok := g.plans[e.PlanRef]
		if !ok {
			return fmt.Errorf("挂接的计划不存在: %s", e.PlanRef)
		}
		r, ok := g.relations[e.RelationRef]
		if !ok {
			return fmt.Errorf("挂接的关系不存在: %s", e.RelationRef)
		}
		if !live(r.view.Status) {
			return fmt.Errorf("关系已终止，不能挂接计划")
		}
		if !g.planCoversRelation(plan, r) {
			return fmt.Errorf("计划 %s 的实体范围不覆盖关系 %s", e.PlanRef, e.RelationRef)
		}
	case events.CityPlanConfirmed:
		r, ok := g.relations[e.RelationRef]
		if !ok {
			return fmt.Errorf("确认对应的关系不存在: %s", e.RelationRef)
		}
		plan, ok := g.plans[e.PlanRef]
		if !ok {
			return fmt.Errorf("确认对应的计划不存在: %s", e.PlanRef)
		}
		if _, bound := plan.RelationConfirmations[e.RelationRef]; !bound {
			return fmt.Errorf("计划 %s 尚未挂接到关系 %s，城市不能确认", e.PlanRef, e.RelationRef)
		}
		if e.CityRef != r.view.EntityA && e.CityRef != r.view.EntityB {
			return fmt.Errorf("确认城市 %s 不是关系的缔约方", e.CityRef)
		}
	case events.CommitmentLogged:
		if _, ok := g.relations[e.RelationRef]; !ok {
			return fmt.Errorf("承诺对应的关系不存在: %s", e.RelationRef)
		}
		if _, dup := g.commitments[e.Ref]; dup {
			return fmt.Errorf("承诺标识已存在: %s", e.Ref)
		}
		// 承诺是事实性记录，允许在关系终止后补录历史事项；
		// 卷宗会结合关系状态展示这些遗留承诺。
		if e.PlanRef != "" {
			if _, ok := g.plans[e.PlanRef]; !ok {
				return fmt.Errorf("承诺关联的计划不存在: %s", e.PlanRef)
			}
		}
	case events.CommitmentFulfilled:
		c, ok := g.commitments[e.Ref]
		if !ok {
			return fmt.Errorf("兑现的承诺不存在: %s", e.Ref)
		}
		if c.Fulfilled {
			return fmt.Errorf("承诺 %s 已标记兑现，不能重复兑现", e.Ref)
		}
	case events.ActivityHeld:
		if _, ok := g.relations[e.RelationRef]; !ok {
			return fmt.Errorf("活动对应的关系不存在: %s", e.RelationRef)
		}
		if _, dup := g.activities[e.Ref]; dup {
			return fmt.Errorf("活动标识已存在: %s", e.Ref)
		}
		for _, ref := range e.CommitmentRefs {
			c, ok := g.commitments[ref]
			if !ok {
				return fmt.Errorf("活动关联的承诺不存在: %s", ref)
			}
			if c.RelationRef != e.RelationRef {
				return fmt.Errorf("活动关联的承诺 %s 不属于该关系", ref)
			}
		}
	default:
		return fmt.Errorf("未知事件类型 %T", p)
	}
	return nil
}

// bothSidesApproposed 核对双边批准：两侧最新决定均为 approved。
func (g *Graph) bothSidesApproved(r *relation) bool {
	a, oka := r.approvals[domain.SideA]
	b, okb := r.approvals[domain.SideB]
	return oka && okb &&
		a.Decision == domain.ApprovalApproved &&
		b.Decision == domain.ApprovalApproved
}

// planCoversRelation 检查计划两侧与关系两侧实体相等或存在行政隶属。
// 上级路线图（国家/省州）因此可以约束下级城市交往计划。
func (g *Graph) planCoversRelation(plan *PlanView, r *relation) bool {
	return g.covers(plan.EntityA, r.view.EntityA) && g.covers(plan.EntityB, r.view.EntityB) ||
		g.covers(plan.EntityA, r.view.EntityB) && g.covers(plan.EntityB, r.view.EntityA)
}

func (g *Graph) covers(upper, lower string) bool {
	if upper == lower {
		return true
	}
	ent, ok := g.entities[lower]
	for ok && ent.view.ParentRef != "" {
		if ent.view.ParentRef == upper {
			return true
		}
		next, exists := g.entities[ent.view.ParentRef]
		if !exists {
			return false
		}
		ent = next
	}
	return false
}

// Resolve 沿合并继受链返回实体当前有效的标识；未合并时返回自身。
func (g *Graph) Resolve(ref string) string {
	ent, ok := g.entities[ref]
	if !ok {
		return ref
	}
	seen := map[string]bool{ref: true}
	for ent.mergedInto != "" {
		if seen[ent.mergedInto] {
			return ent.view.Ref // 继受链成环属于损坏数据，止于当前节点
		}
		seen[ent.mergedInto] = true
		next, ok := g.entities[ent.mergedInto]
		if !ok {
			return ent.mergedInto
		}
		ent = next
	}
	return ent.view.Ref
}

// predecessors 返回直接或间接并入 ref 的全部历史实体（继受链闭包）。
func (g *Graph) predecessors(ref string) []string {
	var out []string
	for candidate := range g.entities {
		if candidate != ref && g.Resolve(candidate) == ref {
			out = append(out, candidate)
		}
	}
	sort.Strings(out)
	return out
}
