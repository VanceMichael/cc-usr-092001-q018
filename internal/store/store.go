// Package store 把追加式事件日志与内存投影组装为单一读写入口：
// 写入先经投影校验，再追加日志，最后更新投影；读取在投影上进行。
// 服务重启时通过重放日志恢复全部状态。
package store

import (
	"fmt"
	"sync"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/eventlog"
	"example.com/batch-092001-q018/internal/events"
	"example.com/batch-092001-q018/internal/graph"
)

// Store 持有日志与读模型。
type Store struct {
	log *eventlog.Log

	mu sync.RWMutex
	g  *graph.Graph
}

// Open 打开日志路径，校验完整性并重放建立投影。
func Open(path string) (*Store, error) {
	lg, err := eventlog.Open(path)
	if err != nil {
		return nil, err
	}
	g := graph.New()
	if err := lg.Replay(func(env events.Envelope, p events.Payload) error {
		return g.Apply(p, env.OccurredAt)
	}); err != nil {
		return nil, fmt.Errorf("重建投影失败: %w", err)
	}
	return &Store{log: lg, g: g}, nil
}

// Commit 把一条领域事件组装为信封并提交：
//  1. 投影业务校验（不合法则不写日志）；
//  2. 追加日志（事件标识幂等、来源序号连续、摘要落盘）；
//  3. 更新内存投影。
//
// duplicated 为 true 时表示相同事件重试，投影与日志均无新增；
// assigned 是事件最终采用的来源序号（0 表示由日志自动分配时也会回填）。
func (s *Store) Commit(eventID, source string, sequence int64, occurredAt string, p events.Payload) (duplicated bool, assigned int64, err error) {
	if err := domain.RequireNonEmpty(eventID, "event_id"); err != nil {
		return false, 0, err
	}
	if _, err := domain.ParseInstant(occurredAt, "occurred_at"); err != nil {
		return false, 0, err
	}
	raw, err := events.EncodePayload(p)
	if err != nil {
		return false, 0, err
	}
	digest := eventlog.Digest(raw)

	s.mu.Lock()
	defer s.mu.Unlock()

	// 幂等优先：同内容重试在任何业务规则校验之前放行，
	// 避免“重试时对象已存在”这类次生冲突。
	dup, assigned, err := s.log.CheckDuplicate(eventID, source, sequence, digest)
	if err != nil {
		return false, 0, err
	}
	if dup {
		return true, assigned, nil
	}

	// 再对当前投影做业务校验，避免无效事件落盘。
	if err := s.g.Validate(p); err != nil {
		return false, 0, err
	}
	env := events.Envelope{
		SchemaVersion:  domain.SchemaVersion,
		EventID:        eventID,
		Source:         source,
		SourceSequence: sequence,
		SubjectRef:     subjectOf(p),
		OccurredAt:     occurredAt,
		PayloadDigest:  digest,
		Payload:        raw,
	}
	dup, assigned, err = s.log.Append(env)
	if err != nil {
		return false, 0, err
	}
	if dup {
		return true, assigned, nil
	}
	if err := s.g.Apply(p, occurredAt); err != nil {
		// 理论上不可达：Validate 已通过且 Append 已落盘。
		return false, assigned, fmt.Errorf("投影更新失败（日志已写入 %s）: %w", eventID, err)
	}
	return false, assigned, nil
}

// IngestEnvelope 接收外部组装好的交换信封（契约见 contracts/）。
// 信封必须自带正确的载荷摘要；处理路径与本地提交一致。
func (s *Store) IngestEnvelope(env events.Envelope) (duplicated bool, err error) {
	_, p, err := events.DecodePayload(env.Payload)
	if err != nil {
		return false, err
	}
	if env.PayloadDigest != eventlog.Digest(env.Payload) {
		return false, fmt.Errorf("载荷摘要不匹配: 信封 %s", env.PayloadDigest)
	}
	if _, err := domain.ParseInstant(env.OccurredAt, "occurred_at"); err != nil {
		return false, err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	// 幂等优先于业务校验。
	dup, assigned, err := s.log.CheckDuplicate(env.EventID, env.Source, env.SourceSequence, env.PayloadDigest)
	if err != nil {
		return false, err
	}
	if dup {
		return true, nil
	}
	if err := s.g.Validate(p); err != nil {
		return false, err
	}
	dup, _, err = s.log.Append(env)
	if err != nil {
		return false, err
	}
	if dup {
		return true, nil
	}
	_ = assigned
	if err := s.g.Apply(p, env.OccurredAt); err != nil {
		return false, fmt.Errorf("投影更新失败（日志已写入 %s）: %w", env.EventID, err)
	}
	return false, nil
}

// View 在读锁内执行投影查询。
func View[T any](s *Store, fn func(*graph.Graph) (T, error)) (T, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return fn(s.g)
}

// Replay 对外暴露只读事件流（交换与审计用）。
func (s *Store) Replay(visit func(events.Envelope, events.Payload) error) error {
	return s.log.Replay(visit)
}

// LastSequence 返回某来源的序号水位，供交换方续传。
func (s *Store) LastSequence(source string) int64 {
	return s.log.LastSequence(source)
}

// subjectOf 从载荷推导事件主引用，便于信封检索。
func subjectOf(p events.Payload) string {
	switch e := p.(type) {
	case events.EntityRegistered:
		return e.Ref
	case events.NameAttached:
		return e.EntityRef
	case events.EntityMerged:
		return e.FromRef
	case events.RelationProposed:
		return e.Ref
	case events.RelationConcluded:
		return e.Ref
	case events.RelationSuspended:
		return e.Ref
	case events.RelationResumed:
		return e.Ref
	case events.RelationTerminated:
		return e.Ref
	case events.ApprovalRecorded:
		return e.RelationRef
	case events.TextRecorded:
		return e.RelationRef
	case events.PlanAdopted:
		return e.Ref
	case events.PlanBoundToRelation:
		return e.RelationRef
	case events.CityPlanConfirmed:
		return e.RelationRef
	case events.CommitmentLogged:
		return e.RelationRef
	case events.CommitmentFulfilled:
		return e.Ref
	case events.ActivityHeld:
		return e.RelationRef
	default:
		return ""
	}
}
