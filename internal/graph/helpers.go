package graph

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

// ErrNotFound 表示引用的对象在投影中不存在。
var ErrNotFound = errors.New("对象不存在")

// ErrSameEntity 表示提议两侧指向同一实体。
var errSameEntity = errors.New("关系两侧不能为同一实体")

func missingEntityError(ref string) error {
	return fmt.Errorf("%w: 实体 %s", ErrNotFound, ref)
}

func lowerTrim(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

// sortIssues 按严重程度与种类排序，保证分析输出稳定可比对。
func sortIssues(issues []ProposalIssue) {
	severityRank := map[string]int{"conflict": 0, "info": 1}
	sort.Slice(issues, func(i, j int) bool {
		if severityRank[issues[i].Severity] != severityRank[issues[j].Severity] {
			return severityRank[issues[i].Severity] < severityRank[issues[j].Severity]
		}
		if issues[i].Kind != issues[j].Kind {
			return issues[i].Kind < issues[j].Kind
		}
		return issues[i].EntityRef+issues[i].RelationRef < issues[j].EntityRef+issues[j].RelationRef
	})
}
