// Package eventlog 是图谱的追加式事实日志。每条事件独占一行 JSON，
// 落盘前计算 sha256 摘要，重放时逐行复核；事件标识在日志范围内幂等，
// 来源序号只在同一来源内严格递增，重复上报不会产生第二条事实。
package eventlog

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"

	"example.com/batch-092001-q018/internal/domain"
	"example.com/batch-092001-q018/internal/events"
)

// ErrDuplicateEvent 表示同一 event_id 被再次提交。内容一致时视为重试，
// 由 Log.Append 幂等返回；内容不一致则作为冲突上报。
var ErrDuplicateEvent = errors.New("事件标识重复")

// recorded 保存用于幂等校验的最小信息。
type recorded struct {
	source   string
	sequence int64
	digest   string
}

// Log 是一个 JSONL 追加日志，索引在打开时一次性建立。
type Log struct {
	path string
	mu   sync.Mutex
	ids  map[string]recorded
	seq  map[string]int64
}

// Open 打开（必要时创建）日志文件，扫描既有内容建立索引并复核摘要。
func Open(path string) (*Log, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("创建日志目录失败: %w", err)
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, fmt.Errorf("打开日志失败: %w", err)
	}
	defer file.Close()

	l := &Log{path: path, ids: make(map[string]recorded), seq: make(map[string]int64)}
	if err := l.scan(file); err != nil {
		return nil, err
	}
	return l, nil
}

// LastSequence 返回某来源已落盘的最大序号，没有记录时为 0。
func (l *Log) LastSequence(source string) int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.seq[source]
}

// CheckDuplicate 在不写入的前提下探测 event_id 是否已落盘，
// 使业务层可以在执行业务规则校验之前先放行同内容重试：
//   - 已存在且来源与摘要一致（自动序号或显式序号相同）→ duplicated=true；
//   - 已存在但来源、序号或内容不一致 → ErrDuplicateEvent；
//   - 不存在 → duplicated=false。
//
// 调用方（store）在自身互斥锁内串联 CheckDuplicate 与 Append，
// 两次调用之间不会有其他写入插入。
func (l *Log) CheckDuplicate(eventID, source string, seq int64, digest string) (duplicated bool, assigned int64, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	prior, ok := l.ids[eventID]
	if !ok {
		return false, 0, nil
	}
	auto := seq == 0
	if prior.digest == digest && prior.source == source && (auto || prior.sequence == seq) {
		return true, prior.sequence, nil
	}
	return false, 0, fmt.Errorf("%w: %s 的来源、序号或内容与首次提交不一致", ErrDuplicateEvent, eventID)
}

// Append 追加一条事件。信封字段由调用方（store 层）组装，这里负责：
//   - 复核载荷自检与摘要；
//   - event_id 幂等（同内容重试直接返回，不同内容报冲突）；
//   - 同一来源的 source_sequence 必须连续递增；序号为 0 时自动分配；
//   - 写入单行并 fsync，同时更新内存索引。
//
// 返回值 duplicated 为 true 时表示幂等命中（重试未产生新事实）；
// assigned 为该事件最终采用的来源序号。
func (l *Log) Append(env events.Envelope) (duplicated bool, assigned int64, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if env.SchemaVersion != domain.SchemaVersion {
		return false, 0, fmt.Errorf("不支持的 schema_version: %q", env.SchemaVersion)
	}
	if env.Source == "" {
		return false, 0, errors.New("事件来源 source 不能为空")
	}
	auto := env.SourceSequence == 0
	if !auto && env.SourceSequence < 1 {
		return false, 0, fmt.Errorf("来源序号必须从 1 开始（或留空自动分配），收到 %d", env.SourceSequence)
	}
	if _, _, err := events.DecodePayload(env.Payload); err != nil {
		return false, 0, err
	}
	if want := Digest(env.Payload); env.PayloadDigest != want {
		return false, 0, fmt.Errorf("载荷摘要不匹配: 信封 %s, 实得 %s", env.PayloadDigest, want)
	}

	if prior, ok := l.ids[env.EventID]; ok {
		if prior.digest == env.PayloadDigest && (auto || prior.sequence == env.SourceSequence) && prior.source == env.Source {
			return true, prior.sequence, nil // 同内容重试，幂等成功
		}
		return false, 0, fmt.Errorf("%w: %s 的来源、序号或内容与首次提交不一致", ErrDuplicateEvent, env.EventID)
	}
	next := l.seq[env.Source] + 1
	if !auto && env.SourceSequence != next {
		return false, 0, fmt.Errorf("来源 %s 的序号不连续: 期望 %d, 收到 %d", env.Source, next, env.SourceSequence)
	}
	env.SourceSequence = next

	line, err := json.Marshal(env)
	if err != nil {
		return false, 0, err
	}
	line = append(line, '\n')

	file, err := os.OpenFile(l.path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return false, 0, fmt.Errorf("打开日志追加失败: %w", err)
	}
	defer file.Close()
	if _, err := file.Write(line); err != nil {
		return false, 0, fmt.Errorf("写入事件失败: %w", err)
	}
	if err := file.Sync(); err != nil {
		return false, 0, fmt.Errorf("持久化事件失败: %w", err)
	}
	l.ids[env.EventID] = recorded{source: env.Source, sequence: env.SourceSequence, digest: env.PayloadDigest}
	l.seq[env.Source] = env.SourceSequence
	return false, env.SourceSequence, nil
}

// scan 通读日志，复核摘要并建立索引。
func (l *Log) scan(file *os.File) error {
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("定位日志起点失败: %w", err)
	}
	reader := bufio.NewReader(file)
	for lineNo := 1; ; lineNo++ {
		line, rerr := reader.ReadBytes('\n')
		if len(line) > 0 {
			var env events.Envelope
			if err := json.Unmarshal(line, &env); err != nil {
				return fmt.Errorf("日志第 %d 行不是合法事件信封: %w", lineNo, err)
			}
			if env.PayloadDigest != Digest(env.Payload) {
				return fmt.Errorf("日志第 %d 行载荷摘要校验失败", lineNo)
			}
			l.ids[env.EventID] = recorded{source: env.Source, sequence: env.SourceSequence, digest: env.PayloadDigest}
			if env.SourceSequence > l.seq[env.Source] {
				l.seq[env.Source] = env.SourceSequence
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return fmt.Errorf("扫描日志第 %d 行失败: %w", lineNo, rerr)
		}
	}
	return nil
}

// Replay 按落盘顺序重放全部事件，逐行复核摘要后调用 visit。
func (l *Log) Replay(visit func(events.Envelope, events.Payload) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	file, err := os.Open(l.path)
	if err != nil {
		return fmt.Errorf("读取日志失败: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	for lineNo := 1; ; lineNo++ {
		line, rerr := reader.ReadBytes('\n')
		if len(line) > 0 {
			var env events.Envelope
			if err := json.Unmarshal(line, &env); err != nil {
				return fmt.Errorf("日志第 %d 行损坏: %w", lineNo, err)
			}
			if env.PayloadDigest != Digest(env.Payload) {
				return fmt.Errorf("日志第 %d 行摘要不匹配", lineNo)
			}
			_, payload, err := events.DecodePayload(env.Payload)
			if err != nil {
				return fmt.Errorf("日志第 %d 行: %w", lineNo, err)
			}
			if err := visit(env, payload); err != nil {
				return fmt.Errorf("重放第 %d 行失败: %w", lineNo, err)
			}
		}
		if errors.Is(rerr, io.EOF) {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	return nil
}

// Digest 返回载荷的 sha256 摘要字符串，格式与契约示例一致（sha256:hex）。
func Digest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
