package service

import (
	"sort"
	"time"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/store"
)

// PairView 是沿任一友城对聚合的完整工作视图。
type PairView struct {
	Relation      *domain.Relation        `json:"relation"`
	Parties       []*domain.Entity        `json:"parties"`
	Commitments   []CommitmentView        `json:"commitments"`
	Activities    []*domain.Activity      `json:"activities"`
	Plans         []*domain.Plan          `json:"plans"`
	Summary       PairSummary             `json:"summary"`
}

// CommitmentView 在承诺上附带运行期判定的逾期标志。
type CommitmentView struct {
	*domain.Commitment
	Overdue bool `json:"overdue"`
}

// PairSummary 汇总该友城对的执行情况。
type PairSummary struct {
	TotalCommitments     int            `json:"total_commitments"`
	OpenCommitments      int            `json:"open_commitments"`
	FulfilledCommitments int            `json:"fulfilled_commitments"`
	OverdueCommitments   int            `json:"overdue_commitments"`
	TotalActivities      int            `json:"total_activities"`
	TotalOutcomes        int            `json:"total_outcomes"`
	ByCategory           map[string]int `json:"by_category"`
}

// GetRelationPair 按关系引用返回友城对聚合视图，now 用于判定逾期（零值取 time.Now）。
func (s *Service) GetRelationPair(relationRef string, now time.Time) (PairView, error) {
	g := s.snapshot()
	g.RLock()
	defer g.RUnlock()

	r, ok := g.Relation(relationRef)
	if !ok {
		return PairView{}, domain.NewError(domain.CodeUnknownRelation, "关系 %s 不存在", relationRef)
	}
	if now.IsZero() {
		now = time.Now()
	}
	return buildPairView(g, r, now), nil
}

// FindPairByEntities 按两个实体引用查找其当前友城对视图（自动沿合并指针解析）。
func (s *Service) FindPairByEntities(a, b string, now time.Time) (PairView, bool, error) {
	g := s.snapshot()
	g.RLock()
	defer g.RUnlock()

	ea := g.ResolveEntity(a)
	eb := g.ResolveEntity(b)
	if ea == nil {
		return PairView{}, false, domain.NewError(domain.CodeUnknownEntity, "实体 %s 不存在", a)
	}
	if eb == nil {
		return PairView{}, false, domain.NewError(domain.CodeUnknownEntity, "实体 %s 不存在", b)
	}
	_, r, ok := g.RelationByPair(ea.Ref, eb.Ref)
	if !ok {
		return PairView{}, false, nil
	}
	if now.IsZero() {
		now = time.Now()
	}
	return buildPairView(g, r, now), true, nil
}

func buildPairView(g *store.Graph, r *domain.Relation, now time.Time) PairView {
	view := PairView{Relation: r, Summary: PairSummary{ByCategory: map[string]int{}}}

	if e, ok := g.Entity(r.PartyA); ok {
		view.Parties = append(view.Parties, e)
	}
	if e, ok := g.Entity(r.PartyB); ok {
		view.Parties = append(view.Parties, e)
	}

	for _, c := range g.CommitmentsOf(r.Ref) {
		overdue := c.IsOverdueAt(now)
		view.Commitments = append(view.Commitments, CommitmentView{Commitment: c, Overdue: overdue})
		view.Summary.TotalCommitments++
		switch {
		case c.Status == domain.CommitFulfilled:
			view.Summary.FulfilledCommitments++
		case c.Status == domain.CommitCancelled:
			// 不计入未完成
		default:
			view.Summary.OpenCommitments++
		}
		if overdue {
			view.Summary.OverdueCommitments++
		}
		view.Summary.ByCategory[string(c.Category)]++
	}
	sort.SliceStable(view.Commitments, func(i, j int) bool {
		return view.Commitments[i].CreatedAt.Before(view.Commitments[j].CreatedAt)
	})

	for _, a := range g.ActivitiesOf(r.Ref) {
		view.Activities = append(view.Activities, a)
		view.Summary.TotalActivities++
		view.Summary.TotalOutcomes += len(a.Outcomes)
	}
	sort.SliceStable(view.Activities, func(i, j int) bool {
		return view.Activities[i].StartAt.Before(view.Activities[j].StartAt)
	})

	for _, p := range g.Plans() {
		if p.RelationRef == r.Ref {
			view.Plans = append(view.Plans, p)
		}
	}
	return view
}

// NameResolution 描述按名称解析实体的结果，含同名歧义与合并承接提示。
type NameResolution struct {
	Query        string         `json:"query"`
	Matches      []ResolvedEntity `json:"matches"`
	Ambiguous    bool           `json:"ambiguous"`
}

// ResolvedEntity 是名称解析命中的实体及其当前承接主体。
type ResolvedEntity struct {
	Ref          string      `json:"ref"`
	Level        domain.Level `json:"level"`
	CurrentName  string      `json:"current_name"`
	MatchedOn    string      `json:"matched_on"` // current | historical
	MergedInto   string      `json:"merged_into,omitempty"`
	CurrentRef   string      `json:"current_ref"` // 解析合并后的当前实体
}

// ResolveName 按当前或历史名称查找实体；同名多个时置 Ambiguous。
func (s *Service) ResolveName(name string) NameResolution {
	g := s.snapshot()
	g.RLock()
	defer g.RUnlock()

	res := NameResolution{Query: name}
	for _, ref := range g.FindByName(name) {
		e, ok := g.Entity(ref)
		if !ok {
			continue
		}
		matchedOn := "historical"
		if domain.NormalizeName(e.CurrentName) == domain.NormalizeName(name) {
			matchedOn = "current"
		}
		currentRef := ref
		if cur := g.ResolveEntity(ref); cur != nil {
			currentRef = cur.Ref
		}
		res.Matches = append(res.Matches, ResolvedEntity{
			Ref: ref, Level: e.Level, CurrentName: e.CurrentName,
			MatchedOn: matchedOn, MergedInto: e.MergedInto, CurrentRef: currentRef,
		})
	}
	sort.SliceStable(res.Matches, func(i, j int) bool { return res.Matches[i].Ref < res.Matches[j].Ref })
	res.Ambiguous = len(res.Matches) > 1
	return res
}

// GetEntity 返回实体及其（合并后的）当前承接信息。
func (s *Service) GetEntity(ref string) (*domain.Entity, error) {
	g := s.snapshot()
	g.RLock()
	defer g.RUnlock()
	e, ok := g.Entity(ref)
	if !ok {
		return nil, domain.NewError(domain.CodeUnknownEntity, "实体 %s 不存在", ref)
	}
	return e, nil
}

// ListedEvent 是事件列表项。
type ListedEvent struct {
	EventID    string `json:"event_id"`
	Source     string `json:"source"`
	EventType  string `json:"event_type"`
	SubjectRef string `json:"subject_ref"`
	OccurredAt string `json:"occurred_at"`
}

// ListEvents 按接纳顺序返回事件元信息。
func (s *Service) ListEvents(limit int) []ListedEvent {
	events := s.log.Events()
	if limit <= 0 || limit > len(events) {
		limit = len(events)
	}
	out := make([]ListedEvent, 0, limit)
	for i := len(events) - limit; i < len(events); i++ {
		e := events[i].Envelope
		out = append(out, ListedEvent{
			EventID: e.EventID, Source: e.Source, EventType: e.EventType,
			SubjectRef: e.SubjectRef, OccurredAt: e.OccurredAt,
		})
	}
	return out
}
