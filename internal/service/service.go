// Package service 编排图谱的对外业务能力：写入统一走事件提交，
// 读取走卷宗与提议分析；同一进程内保证写互斥、读并发。
package service

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/eventlog"
	"example.com/batch-092001-q018/internal/events"
	"example.com/batch-092001-q018/internal/graph"
	"example.com/batch-092001-q018/internal/store"
)

// Service 是业务服务。
type Service struct {
	store       *store.Store
	localSource string
	now         func() time.Time
}

// New 装配服务。localSource 为空时使用默认来源 "municipal-affairs-office"。
func New(st *store.Store, localSource string) *Service {
	if localSource == "" {
		localSource = "municipal-affairs-office"
	}
	return &Service{store: st, localSource: localSource, now: time.Now}
}

// CommitMeta 携带一次写入的交换元信息。调用方可全部省略，
// 由服务补齐事件标识、本地来源、连续序号与当前时刻。
type CommitMeta struct {
	EventID        string `json:"event_id,omitempty"`
	Source         string `json:"source,omitempty"`
	SourceSequence int64  `json:"source_sequence,omitempty"`
	OccurredAt     string `json:"occurred_at,omitempty"`
}

// CommitResult 回报事件落盘结果；Duplicated=true 表示幂等重试。
type CommitResult struct {
	EventID        string `json:"event_id"`
	Source         string `json:"source"`
	SourceSequence int64  `json:"source_sequence"`
	Duplicated     bool   `json:"duplicated"`
}

// ValidationError 包装请求形状错误（对应 HTTP 400）。
type ValidationError struct{ Err error }

func (e *ValidationError) Error() string { return e.Err.Error() }
func (e *ValidationError) Unwrap() error { return e.Err }

// ConflictError 包装业务规则冲突（对应 HTTP 409/422）。
type ConflictError struct{ Err error }

func (e *ConflictError) Error() string { return e.Err.Error() }
func (e *ConflictError) Unwrap() error { return e.Err }

// NotFoundError 包装对象不存在（对应 HTTP 404）。
type NotFoundError struct{ Err error }

func (e *NotFoundError) Error() string { return e.Err.Error() }
func (e *NotFoundError) Unwrap() error { return e.Err }

// Commit 提交一条领域事件。
func (s *Service) Commit(meta CommitMeta, p events.Payload) (CommitResult, error) {
	if err := p.Validate(); err != nil {
		return CommitResult{}, &ValidationError{Err: err}
	}
	source := meta.Source
	if source == "" {
		source = s.localSource
	}
	occurredAt := meta.OccurredAt
	if occurredAt == "" {
		occurredAt = s.now().UTC().Format(time.RFC3339)
	}
	eventID := meta.EventID
	if eventID == "" {
		id, err := newEventID()
		if err != nil {
			return CommitResult{}, err
		}
		eventID = id
	}
	// source_sequence 留空（0）时由日志在写锁内自动分配连续序号。
	dup, assigned, err := s.store.Commit(eventID, source, meta.SourceSequence, occurredAt, p)
	if err != nil {
		return CommitResult{}, &ConflictError{Err: err}
	}
	return CommitResult{EventID: eventID, Source: source, SourceSequence: assigned, Duplicated: dup}, nil
}

// Ingest 接收外部交换信封（契约见 contracts/）。
func (s *Service) Ingest(env events.Envelope) (CommitResult, error) {
	dup, err := s.store.IngestEnvelope(env)
	if err != nil {
		if errors.Is(err, eventlog.ErrDuplicateEvent) {
			return CommitResult{}, &ConflictError{Err: err}
		}
		return CommitResult{}, &ConflictError{Err: err}
	}
	return CommitResult{
		EventID: env.EventID, Source: env.Source,
		SourceSequence: env.SourceSequence, Duplicated: dup,
	}, nil
}

// ExportEvents 只读重放全部事件。
func (s *Service) ExportEvents(visit func(events.Envelope, events.Payload) error) error {
	return s.store.Replay(visit)
}

// ---- 读模型查询 ----

// EntityDossier 返回实体卷宗。
func (s *Service) EntityDossier(ref string) (graph.EntityDossier, error) {
	if err := domain.CheckRef(ref, domain.PrefixEntity); err != nil {
		return graph.EntityDossier{}, &ValidationError{Err: err}
	}
	d, err := store.View(s.store, func(g *graph.Graph) (graph.EntityDossier, error) {
		d, ok := g.DossierEntity(ref)
		if !ok {
			return graph.EntityDossier{}, fmt.Errorf("%w: 实体 %s", graph.ErrNotFound, ref)
		}
		return d, nil
	})
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			return graph.EntityDossier{}, &NotFoundError{Err: err}
		}
		return graph.EntityDossier{}, &ConflictError{Err: err}
	}
	return d, nil
}

// RelationDossier 返回友城对卷宗，逾期比较使用服务当前时刻。
func (s *Service) RelationDossier(ref string) (graph.RelationDossier, error) {
	if err := domain.CheckRef(ref, domain.PrefixRelation); err != nil {
		return graph.RelationDossier{}, &ValidationError{Err: err}
	}
	now := s.now().UTC()
	d, err := store.View(s.store, func(g *graph.Graph) (graph.RelationDossier, error) {
		d, ok := g.DossierRelation(ref, now)
		if !ok {
			return graph.RelationDossier{}, fmt.Errorf("%w: 关系 %s", graph.ErrNotFound, ref)
		}
		return d, nil
	})
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			return graph.RelationDossier{}, &NotFoundError{Err: err}
		}
		return graph.RelationDossier{}, &ConflictError{Err: err}
	}
	return d, nil
}

// AnalyzeProposal 分析新合作提议与现有网络的重叠、冲突与可复用资源。
func (s *Service) AnalyzeProposal(entityA, entityB string) (graph.ProposalAnalysis, error) {
	if err := domain.CheckRef(entityA, domain.PrefixEntity); err != nil {
		return graph.ProposalAnalysis{}, &ValidationError{Err: fmt.Errorf("entity_a 不合法: %w", err)}
	}
	if err := domain.CheckRef(entityB, domain.PrefixEntity); err != nil {
		return graph.ProposalAnalysis{}, &ValidationError{Err: fmt.Errorf("entity_b 不合法: %w", err)}
	}
	res, err := store.View(s.store, func(g *graph.Graph) (graph.ProposalAnalysis, error) {
		return g.AnalyzeProposal(entityA, entityB)
	})
	if err != nil {
		if errors.Is(err, graph.ErrNotFound) {
			return graph.ProposalAnalysis{}, &NotFoundError{Err: err}
		}
		return graph.ProposalAnalysis{}, &ConflictError{Err: err}
	}
	return res, nil
}

// SearchEntities 按语言与名称片段查找实体，用于录入前核对同名城市。
func (s *Service) SearchEntities(lang, name string) ([]graph.EntityView, error) {
	if err := domain.RequireNonEmpty(lang, "lang"); err != nil {
		return nil, &ValidationError{Err: err}
	}
	if err := domain.RequireNonEmpty(name, "name"); err != nil {
		return nil, &ValidationError{Err: err}
	}
	return store.View(s.store, func(g *graph.Graph) ([]graph.EntityView, error) {
		return g.SearchEntities(lang, name)
	})
}

// ListRelations 列出全部关系（按引用排序）。
func (s *Service) ListRelations() ([]graph.RelationView, error) {
	return store.View(s.store, func(g *graph.Graph) ([]graph.RelationView, error) {
		return g.ListRelations(), nil
	})
}

func newEventID() (string, error) {
	var buf [12]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", fmt.Errorf("生成事件标识失败: %w", err)
	}
	return "EVT-" + hex.EncodeToString(buf[:]), nil
}
