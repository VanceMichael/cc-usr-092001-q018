// Package domain 保存友城关系图谱的基础标识、枚举、校验与时间约定。
//
// 标识一经分配不得复用：行政区更名或合并时旧标识继续指向历史事实，
// 关系记录通过实体引用追溯到当时的名称与继受链。
package domain

import (
	"fmt"
	"regexp"
	"strings"
	"time"
)

// SchemaVersion 是事件信封的当前契约版本，对应 contracts/event.example.json。
const SchemaVersion = "1"

// EntityLevel 是行政实体的层级。
type EntityLevel string

const (
	LevelCountry EntityLevel = "country" // 国家
	LevelState   EntityLevel = "state"   // 省州
	LevelCity    EntityLevel = "city"    // 城市
)

// Valid 报告层级是否受控。
func (l EntityLevel) Valid() bool {
	switch l {
	case LevelCountry, LevelState, LevelCity:
		return true
	default:
		return false
	}
}

// RelationStatus 是友好关系的生命周期状态。
type RelationStatus string

const (
	StatusIntended   RelationStatus = "intended"   // 意向
	StatusConcluded  RelationStatus = "concluded"  // 正式缔结
	StatusSuspended  RelationStatus = "suspended"  // 暂停
	StatusTerminated RelationStatus = "terminated" // 终止
)

// TextKind 区分多语文本的角色：原文、译文、签署版本。
type TextKind string

const (
	TextOriginal    TextKind = "original"    // 原文
	TextTranslation TextKind = "translation" // 译文
)

// PlanTier 区分上级路线图与下级交往计划。
type PlanTier string

const (
	PlanRoadmap PlanTier = "roadmap" // 上级路线图
	PlanPlan    PlanTier = "plan"    // 下级交往计划
)

// ApprovalSide 标识双边关系中方法定批准的一侧。
type ApprovalSide string

const (
	SideA ApprovalSide = "a"
	SideB ApprovalSide = "b"
)

// ApprovalDecision 是法定批准机关的决定。
type ApprovalDecision string

const (
	ApprovalApproved ApprovalDecision = "approved"
	ApprovalRejected ApprovalDecision = "rejected"
)

// 合作事项类别。题面点名文化、旅游、教育、青年，其余归入 other。
const (
	CatCulture   = "culture"
	CatTourism   = "tourism"
	CatEducation = "education"
	CatYouth     = "youth"
	CatOther     = "other"
)

// ValidCategory 校验合作事项类别。
func ValidCategory(category string) bool {
	switch category {
	case CatCulture, CatTourism, CatEducation, CatYouth, CatOther:
		return true
	default:
		return false
	}
}

// LocalizedText 同时保存一种内容的多个语言版本，键为 BCP 47 语言标签。
// 原文与译文都以同样的结构保存，由文本记录的 kind 区分角色。
type LocalizedText map[string]string

// Name 稳定引用的前缀，便于在卷宗中辨识对象类型。
const (
	PrefixEntity     = "ENT-"
	PrefixRelation   = "REL-"
	PrefixPlan       = "PLN-"
	PrefixCommitment = "CMT-"
	PrefixActivity   = "ACT-"
	PrefixApproval   = "APR-"
	PrefixText       = "TXT-"
)

var refPattern = regexp.MustCompile(`^[A-Z]{3}-[0-9A-Za-z][0-9A-Za-z._-]{2,63}$`)

// CheckRef 校验引用编号形状并核对前缀。
func CheckRef(ref, prefix string) error {
	if !refPattern.MatchString(ref) {
		return fmt.Errorf("引用编号形状不合法: %q", ref)
	}
	if prefix != "" && !strings.HasPrefix(ref, prefix) {
		return fmt.Errorf("引用编号 %q 必须使用前缀 %s", ref, prefix)
	}
	return nil
}

// RequireNonEmpty 校验受控字符串不为空。
func RequireNonEmpty(value, field string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s 不能为空", field)
	}
	return nil
}

// RequireText 校验多语文本至少包含一个非空语言版本。
func RequireText(text LocalizedText, field string) error {
	for lang, value := range text {
		if strings.TrimSpace(lang) == "" {
			return fmt.Errorf("%s 的语言标签不能为空", field)
		}
		if strings.TrimSpace(value) != "" {
			return nil
		}
	}
	return fmt.Errorf("%s 至少需要一个非空语言版本", field)
}

// ParseInstant 解析带偏移量的 ISO 8601 时间；接收方保留原始字符串，
// 解析结果仅用于按绝对时刻排序与逾期计算，不得回写覆盖发生时间。
func ParseInstant(value, field string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, fmt.Errorf("%s 不能为空", field)
	}
	instant, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s 必须是带偏移量的 ISO 8601 时间: %w", field, err)
	}
	return instant, nil
}

// ParseDate 解析日历日期（YYYY-MM-DD）。承诺没有具体时刻时以日期
// 截止，逾期比较按该日期当天结束（23:59:59，保留为 UTC 瞬时）。
func ParseDate(value, field string) (time.Time, error) {
	if strings.TrimSpace(value) == "" {
		return time.Time{}, fmt.Errorf("%s 不能为空", field)
	}
	day, err := time.ParseInLocation("2006-01-02", value, time.UTC)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s 必须是 YYYY-MM-DD 日期: %w", field, err)
	}
	return day.Add(24*time.Hour - time.Second), nil
}

// PairKey 返回无序实体对的稳定键，使同一对城市无论上报顺序如何
// 都只会对应一对关系（重复上报不创建第二对）。
func PairKey(aRef, bRef string) string {
	if aRef < bRef {
		return aRef + "\x00" + bRef
	}
	return bRef + "\x00" + aRef
}

// CanonicalPair 返回规范化顺序的两侧，供展示与存储使用。
func CanonicalPair(aRef, bRef string) (string, string) {
	if aRef < bRef {
		return aRef, bRef
	}
	return bRef, aRef
}
