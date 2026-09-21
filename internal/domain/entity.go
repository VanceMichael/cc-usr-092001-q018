package domain

import (
	"sort"
	"strings"
	"time"
)

// NameAlias 记录一个实体的历史名称或别名。Current=false 表示旧名（因更名产生），
// 旧名长期保留，保证旧关系可追溯。
type NameAlias struct {
	Name     string    `json:"name"`
	Language string    `json:"language,omitempty"`
	FromRaw  string    `json:"from"`                    // 保留带偏移的原始字符串
	From     time.Time `json:"-"`
	ToRaw    string    `json:"to,omitempty"`            // 该名称停止使用的时间（空表示当前）
	To       time.Time `json:"-"`
	Reason   string    `json:"reason,omitempty"`        // rename | merge | split | alias
	Current  bool      `json:"current"`
}

// AdministrativeReorg 描述行政区更名/合并/拆分等调整。
type AdministrativeReorg struct {
	Kind       string   `json:"kind"`        // rename | merge | split
	SourceRefs []string `json:"source_refs"` // 被合并/拆分/更名前的实体
	TargetRef  string   `json:"target_ref"`  // 调整后的承接实体
	Effective  string   `json:"effective"`   // RFC3339 生效时间
	Note       string   `json:"note,omitempty"`
}

// Entity 是国家、省州或城市的稳定标识聚合。
// 引用编号在行政区调整期间保持不变或通过合并指针承接；旧名与旧关系均不删除。
type Entity struct {
	Ref          string                 `json:"ref"`
	Level        Level                  `json:"level"`
	Jurisdiction string                 `json:"jurisdiction,omitempty"` // 所属国家/省州引用
	CurrentName  string                 `json:"current_name"`
	Names        []NameAlias            `json:"names"`
	// MergedInto 非空表示该实体已被合并，指向承接实体；本实体转为只读历史主体。
	MergedInto   string                 `json:"merged_into,omitempty"`
	Reorgs       []AdministrativeReorg  `json:"reorgs,omitempty"`
	CreatedRaw   string                 `json:"created_at"`
	CreatedAt    time.Time              `json:"-"`
	UpdatedRaw   string                 `json:"updated_at"`
	UpdatedAt    time.Time              `json:"-"`
}

// HasName 报告实体当前或历史上是否使用过某名称（规范化后比较，支持多语言原文）。
func (e *Entity) HasName(name string) bool {
	target := NormalizeName(name)
	if target == "" {
		return false
	}
	if NormalizeName(e.CurrentName) == target {
		return true
	}
	for _, n := range e.Names {
		if NormalizeName(n.Name) == target {
			return true
		}
	}
	return false
}

// AddName 在更名/别名事件中追加名称并维护当前标志；同名重复登记被幂等忽略。
// 返回是否实际新增。
func (e *Entity) AddName(name, language, fromRaw string, at time.Time, reason string, current bool) bool {
	for i := range e.Names {
		if NormalizeName(e.Names[i].Name) == NormalizeName(name) {
			if current && !e.Names[i].Current {
				e.Names[i].Current = true
			}
			return false
		}
	}
	if current {
		for i := range e.Names {
			e.Names[i].Current = false
			if e.Names[i].ToRaw == "" {
				e.Names[i].ToRaw = fromRaw
				e.Names[i].To = at
			}
		}
	}
	e.Names = append(e.Names, NameAlias{
		Name: name, Language: language, FromRaw: fromRaw, From: at,
		Reason: reason, Current: current,
	})
	sort.SliceStable(e.Names, func(i, j int) bool { return e.Names[i].From.Before(e.Names[j].From) })
	return true
}

// NormalizeName 规范化城市名称用于同名匹配：去空白、小写。
// 跨语言的等价名称不做折叠，避免把不同语言写法误判为同一城市。
func NormalizeName(name string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(strings.ToLower(name))), " ")
}
