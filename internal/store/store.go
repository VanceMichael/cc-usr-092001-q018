package store

import (
	"sync"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/event"
)

// RecordedEvent 保存一条已接纳事件及其解码后的类型化载荷。
type RecordedEvent struct {
	Envelope event.Envelope
	Payload  any
}

// EventStore 是线程安全的追加式事件日志。
// 规则：
//   - event_id 全局唯一；同一 event_id 携带相同摘要重放视为幂等 no-op，
//     携带不同内容则判为重复事件冲突。
//   - source_sequence 仅在同一 source 内比较，必须严格递增（缺口允许，倒退/重复拒绝）。
type EventStore struct {
	mu        sync.RWMutex
	log       []RecordedEvent
	byID      map[string]int    // event_id -> 日志下标
	lastSeq   map[string]int64  // source -> 已接纳最大序号
}

// NewEventStore 创建空事件存储。
func NewEventStore() *EventStore {
	return &EventStore{byID: map[string]int{}, lastSeq: map[string]int64{}}
}

// AppendResult 描述一次追加的结果。
type AppendResult struct {
	Accepted     bool // false 表示命中幂等重放（同一事件已存在）
	Sequence     int
}

// Append 在通过幂等与序号校验后写入事件。
func (s *EventStore) Append(env event.Envelope, payload any) (AppendResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if idx, ok := s.byID[env.EventID]; ok {
		existing := s.log[idx].Envelope
		if existing.PayloadDigest == env.PayloadDigest && existing.EventType == env.EventType {
			return AppendResult{Accepted: false, Sequence: idx}, nil
		}
		return AppendResult{}, domain.NewError(domain.CodeDuplicateEvent,
			"event_id=%s 已存在但内容/摘要不同，拒绝覆盖", env.EventID)
	}

	if last := s.lastSeq[env.Source]; last != 0 && env.SourceSequence <= last {
		return AppendResult{}, domain.NewError(domain.CodeSequenceConflict,
			"source=%s 的 source_sequence=%d 不大于已接纳最大值 %d", env.Source, env.SourceSequence, last)
	}

	idx := len(s.log)
	s.log = append(s.log, RecordedEvent{Envelope: env, Payload: payload})
	s.byID[env.EventID] = idx
	s.lastSeq[env.Source] = env.SourceSequence
	return AppendResult{Accepted: true, Sequence: idx}, nil
}

// AbortLast 回滚最近一次成功的 Append（投影失败时使用）。
// 仅在写入被串行化、且确认 eventID 确为日志末尾时调用。
func (s *EventStore) AbortLast(eventID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.log) == 0 {
		return
	}
	last := s.log[len(s.log)-1]
	if last.Envelope.EventID != eventID {
		return
	}
	src := last.Envelope.Source
	seq := last.Envelope.SourceSequence
	s.log = s.log[:len(s.log)-1]
	delete(s.byID, eventID)
	// 重算该来源的最大序号。
	var maxSeq int64
	for _, re := range s.log {
		if re.Envelope.Source == src && re.Envelope.SourceSequence > maxSeq {
			maxSeq = re.Envelope.SourceSequence
		}
	}
	if maxSeq == 0 {
		delete(s.lastSeq, src)
	} else {
		s.lastSeq[src] = maxSeq
	}
	_ = seq
}

// Events 返回已接纳事件的只读快照（按接纳顺序）。
func (s *EventStore) Events() []RecordedEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]RecordedEvent, len(s.log))
	copy(out, s.log)
	return out
}

// Len 返回已接纳事件数。
func (s *EventStore) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.log)
}
