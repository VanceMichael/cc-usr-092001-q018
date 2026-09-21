package service

import (
	"sync"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
	"example.com/batch-092001-q018/internal/store"
)

// Service 装配事件日志、图谱投影与写入规则，是 HTTP 层之下的应用边界。
// 写入通过互斥锁串行化；读取使用读锁并基于当前投影快照。
type Service struct {
	mu        sync.RWMutex
	log       *store.EventStore
	graph     *store.Graph
	projector *Projector
	now       func() string // 可注入时钟（RFC3339）
}

// New 创建空服务。
func New() *Service {
	g := store.NewGraph()
	return &Service{
		log:       store.NewEventStore(),
		graph:     g,
		projector: NewProjector(g),
	}
}

// IngestResult 描述一次事件受理结果。
type IngestResult struct {
	EventID  string `json:"event_id"`
	Idempotent bool  `json:"idempotent"` // true 表示同一事件重放，未重复应用
	Sequence int    `json:"sequence"`
}

// Ingest 校验信封、登记事件并应用到投影。
// 同一 event_id+摘要 的重放幂等返回；投影规则失败时回滚日志登记并重建投影，
// 保证被拒绝事件不残留任何状态。
func (s *Service) Ingest(env event.Envelope) (IngestResult, error) {
	payload, err := env.Validate()
	if err != nil {
		return IngestResult{}, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	res, err := s.log.Append(env, payload)
	if err != nil {
		return IngestResult{}, err
	}
	if !res.Accepted {
		return IngestResult{EventID: env.EventID, Idempotent: true, Sequence: res.Sequence}, nil
	}

	if err := s.projector.Apply(env, payload); err != nil {
		// 回滚日志登记，并从既有事件重建一致投影。
		s.log.AbortLast(env.EventID)
		s.rebuildLocked()
		return IngestResult{}, err
	}
	return IngestResult{EventID: env.EventID, Idempotent: false, Sequence: res.Sequence}, nil
}

// rebuildLocked 清空投影并从事件日志完整重放。仅在写入锁内、处理某事件被拒后调用。
func (s *Service) rebuildLocked() {
	g := store.NewGraph()
	p := NewProjector(g)
	for _, re := range s.log.Events() {
		if err := p.Apply(re.Envelope, re.Payload); err != nil {
			// 已受理事件重放必然成功；若失败说明存在不变量缺陷。
			panic("rebuild failed for accepted event " + re.Envelope.EventID + ": " + err.Error())
		}
	}
	s.graph = g
	s.projector = p
}

// snapshot 在读锁内返回当前投影（供查询方法使用）。
func (s *Service) snapshot() *store.Graph {
	s.mu.RLock()
	g := s.graph
	s.mu.RUnlock()
	return g
}

// ensureDomainError 把意外错误规整为 *domain.Error，便于 HTTP 映射。
func ensureDomainError(err error) error {
	if err == nil {
		return nil
	}
	if _, ok := err.(*domain.Error); ok {
		return err
	}
	return domain.NewError(domain.CodeValidation, "%v", err)
}
