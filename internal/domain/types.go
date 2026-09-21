package domain

import (
	"regexp"
	"strings"
	"time"
)

// RefPattern 约束外部稳定引用编号的形状：前缀-数字段-大写字母数字，不含真实身份信息。
var RefPattern = regexp.MustCompile(`^[A-Z][A-Z0-9]{1,15}-[0-9]{3,12}(-[A-Z0-9]{1,20})*$`)

// ValidateRef 校验稳定引用编号。
func ValidateRef(ref, field string) error {
	if ref == "" {
		return NewError(CodeValidation, "%s 不能为空", field)
	}
	if !RefPattern.MatchString(ref) {
		return NewError(CodeValidation, "%s=%q 不符合引用编号约定", field, ref)
	}
	return nil
}

// ParseOccurredAt 按 RFC3339（带偏移量的 ISO 8601）解析时间。
// 领域约定要求保留原始发生时间且支持跨时区活动，因此必须带显式偏移（或 Z），
// 所有存储与比较都基于 time.Time 的绝对时刻。Go 的 RFC3339 布局会拒绝缺省偏移的写法。
func ParseOccurredAt(value string) (time.Time, error) {
	if value == "" {
		return time.Time{}, NewError(CodeValidation, "occurred_at 不能为空")
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return time.Time{}, NewError(CodeValidation, "occurred_at=%q 不是合法的带偏移 RFC3339 时间: %v", value, err)
	}
	return t, nil
}

// LocalizedText 同时保存多语文本。Original 为原文，Translations 为译文，
// SignedVersionRef 指向实际签署/确认的版本；缺省签署版本即原文。
type LocalizedText struct {
	OriginalLanguage string            `json:"original_language"`
	Original         string            `json:"original"`
	Translations     map[string]string `json:"translations,omitempty"` // BCP-47 语言标签 -> 译文
	SignedVersionRef string            `json:"signed_version_ref,omitempty"`
}

// Validate 校验多语文本至少含原文与原文语言。
func (t LocalizedText) Validate(field string) error {
	if strings.TrimSpace(t.Original) == "" {
		return NewError(CodeValidation, "%s.original 不能为空", field)
	}
	if strings.TrimSpace(t.OriginalLanguage) == "" {
		return NewError(CodeValidation, "%s.original_language 不能为空", field)
	}
	return nil
}

// EffectiveSignedText 返回签署版本引用；未单独登记时以原文语言标签充当。
func (t LocalizedText) EffectiveSignedLanguage() string {
	if t.SignedVersionRef != "" {
		return t.SignedVersionRef
	}
	return t.OriginalLanguage
}

// Digest 保存受控材料的 sha256 摘要，形状 sha256:<hex>。
type Digest string

var digestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

// Validate 校验摘要形状。
func (d Digest) Validate(field string) error {
	if d == "" {
		return NewError(CodeValidation, "%s 不能为空", field)
	}
	if !digestPattern.MatchString(string(d)) {
		return NewError(CodeValidation, "%s=%q 必须形如 sha256:<64位十六进制>", field, string(d))
	}
	return nil
}
