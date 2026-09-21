package store

import (
	"sync"

	"example.com/batch-092001-q018/internal/domain"
)

// Graph 是事件日志的内存投影，提供实体、关系、计划、承诺、活动的多维索引。
// 所有读写均通过锁保护；投影本身不做业务判定，只维护可查询状态。
type Graph struct {
	mu sync.RWMutex

	entities  map[string]*domain.Entity
	relations map[string]*domain.Relation
	plans     map[string]*domain.Plan
	commitments map[string]*domain.Commitment
	activities  map[string]*domain.Activity

	// pairIndex: 无序双方键 -> 关系引用，保证重复上报不创建第二对关系。
	pairIndex map[string]string
	// nameIndex: 规范化名称 -> 具备该名称（含历史名）的实体引用集合。
	nameIndex map[string]map[string]struct{}
	// commitmentDedup: relation_ref -> dedup_key -> commitment_ref。
	commitmentDedup map[string]map[string]string
	// relationCommitments / relationActivities: 关系 -> 下属项。
	relationCommitments map[string][]string
	relationActivities  map[string][]string
}

// NewGraph 创建空投影。
func NewGraph() *Graph {
	return &Graph{
		entities:            map[string]*domain.Entity{},
		relations:           map[string]*domain.Relation{},
		plans:               map[string]*domain.Plan{},
		commitments:         map[string]*domain.Commitment{},
		activities:          map[string]*domain.Activity{},
		pairIndex:           map[string]string{},
		nameIndex:           map[string]map[string]struct{}{},
		commitmentDedup:     map[string]map[string]string{},
		relationCommitments: map[string][]string{},
		relationActivities:  map[string][]string{},
	}
}

// Lock/Unlock 供投影应用器在一次事件内做一致性多表变更。
func (g *Graph) Lock()   { g.mu.Lock() }
func (g *Graph) Unlock() { g.mu.Unlock() }
func (g *Graph) RLock()  { g.mu.RLock() }
func (g *Graph) RUnlock() { g.mu.RUnlock() }

// ---- 实体 ----

// PutEntity 存入或替换实体投影。
func (g *Graph) PutEntity(e *domain.Entity) {
	g.entities[e.Ref] = e
	g.indexEntityNames(e)
}

func (g *Graph) indexEntityNames(e *domain.Entity) {
	add := func(name string) {
		k := domain.NormalizeName(name)
		if k == "" {
			return
		}
		if g.nameIndex[k] == nil {
			g.nameIndex[k] = map[string]struct{}{}
		}
		g.nameIndex[k][e.Ref] = struct{}{}
	}
	add(e.CurrentName)
	for _, n := range e.Names {
		add(n.Name)
	}
}

// Entity 返回实体副本指针（投影内对象，调用方在写事务中使用）。
func (g *Graph) Entity(ref string) (*domain.Entity, bool) {
	e, ok := g.entities[ref]
	return e, ok
}

// ResolveEntity 沿合并指针解析到当前承接实体；若中间实体不存在则返回原引用。
func (g *Graph) ResolveEntity(ref string) *domain.Entity {
	visited := map[string]bool{}
	for {
		e, ok := g.entities[ref]
		if !ok {
			return nil
		}
		if e.MergedInto == "" || visited[ref] {
			return e
		}
		visited[ref] = true
		ref = e.MergedInto
	}
}

// Entities 返回全部实体。
func (g *Graph) Entities() []*domain.Entity {
	out := make([]*domain.Entity, 0, len(g.entities))
	for _, e := range g.entities {
		out = append(out, e)
	}
	return out
}

// FindByName 按当前或历史名称解析实体，返回匹配引用集合（同名可能有多个，需人工消歧）。
func (g *Graph) FindByName(name string) []string {
	set := g.nameIndex[domain.NormalizeName(name)]
	if len(set) == 0 {
		return nil
	}
	out := make([]string, 0, len(set))
	for ref := range set {
		out = append(out, ref)
	}
	return out
}

// ---- 关系 ----

// PutRelation 存入关系并维护双方唯一索引。
func (g *Graph) PutRelation(r *domain.Relation) {
	g.relations[r.Ref] = r
	g.pairIndex[domain.PairKey(r.PartyA, r.PartyB)] = r.Ref
}

// Relation 返回关系。
func (g *Graph) Relation(ref string) (*domain.Relation, bool) {
	r, ok := g.relations[ref]
	return r, ok
}

// RelationByPair 按无序双方查找既有关系。
func (g *Graph) RelationByPair(a, b string) (string, *domain.Relation, bool) {
	ref, ok := g.pairIndex[domain.PairKey(a, b)]
	if !ok {
		return "", nil, false
	}
	return ref, g.relations[ref], true
}

// Relations 返回全部关系。
func (g *Graph) Relations() []*domain.Relation {
	out := make([]*domain.Relation, 0, len(g.relations))
	for _, r := range g.relations {
		out = append(out, r)
	}
	return out
}

// RelationsOf 返回某实体（解析合并后）作为一方的全部关系。
func (g *Graph) RelationsOf(ref string) []*domain.Relation {
	resolved := g.ResolveEntity(ref)
	target := ref
	if resolved != nil {
		target = resolved.Ref
	}
	var out []*domain.Relation
	for _, r := range g.relations {
		if r.HasParty(ref) || (target != ref && r.HasParty(target)) {
			out = append(out, r)
		}
	}
	return out
}

// ---- 计划 ----

func (g *Graph) PutPlan(p *domain.Plan) { g.plans[p.Ref] = p }

func (g *Graph) Plan(ref string) (*domain.Plan, bool) {
	p, ok := g.plans[ref]
	return p, ok
}

func (g *Graph) Plans() []*domain.Plan {
	out := make([]*domain.Plan, 0, len(g.plans))
	for _, p := range g.plans {
		out = append(out, p)
	}
	return out
}

// ChildrenOf 返回直接挂在某上级路线图下的计划。
func (g *Graph) ChildrenOf(parentRef string) []*domain.Plan {
	var out []*domain.Plan
	for _, p := range g.plans {
		if p.ParentRef == parentRef {
			out = append(out, p)
		}
	}
	return out
}

// ---- 承诺 ----

// PutCommitment 存入承诺并维护关系内去重索引与关系列表。
func (g *Graph) PutCommitment(c *domain.Commitment) {
	g.commitments[c.Ref] = c
	if g.commitmentDedup[c.RelationRef] == nil {
		g.commitmentDedup[c.RelationRef] = map[string]string{}
	}
	g.commitmentDedup[c.RelationRef][c.DedupKey] = c.Ref
	if !contains(g.relationCommitments[c.RelationRef], c.Ref) {
		g.relationCommitments[c.RelationRef] = append(g.relationCommitments[c.RelationRef], c.Ref)
	}
}

func (g *Graph) Commitment(ref string) (*domain.Commitment, bool) {
	c, ok := g.commitments[ref]
	return c, ok
}

// CommitmentByDedup 按关系内指纹查找既有承诺。
func (g *Graph) CommitmentByDedup(relationRef, dedupKey string) (string, bool) {
	ref, ok := g.commitmentDedup[relationRef][dedupKey]
	return ref, ok
}

func (g *Graph) CommitmentsOf(relationRef string) []*domain.Commitment {
	refs := g.relationCommitments[relationRef]
	out := make([]*domain.Commitment, 0, len(refs))
	for _, ref := range refs {
		if c, ok := g.commitments[ref]; ok {
			out = append(out, c)
		}
	}
	return out
}

// ---- 活动 ----

func (g *Graph) PutActivity(a *domain.Activity) {
	g.activities[a.Ref] = a
	if !contains(g.relationActivities[a.RelationRef], a.Ref) {
		g.relationActivities[a.RelationRef] = append(g.relationActivities[a.RelationRef], a.Ref)
	}
}

func (g *Graph) Activity(ref string) (*domain.Activity, bool) {
	a, ok := g.activities[ref]
	return a, ok
}

func (g *Graph) ActivitiesOf(relationRef string) []*domain.Activity {
	refs := g.relationActivities[relationRef]
	out := make([]*domain.Activity, 0, len(refs))
	for _, ref := range refs {
		if a, ok := g.activities[ref]; ok {
			out = append(out, a)
		}
	}
	return out
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
