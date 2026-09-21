package graph

import "sort"

// SearchEntities 在指定语言下按名称（大小写不敏感）查找实体，
// 包含历史名称；用于录入前核对同名城市与旧名称。
func (g *Graph) SearchEntities(lang, name string) ([]EntityView, error) {
	target := lowerTrim(name)
	var refs []string
	seen := map[string]bool{}
	for key, set := range g.names {
		keyLang, value, ok := splitNameKey(key)
		if !ok || keyLang != lang || value != target {
			continue
		}
		for ref := range set {
			if !seen[ref] {
				seen[ref] = true
				refs = append(refs, ref)
			}
		}
	}
	sort.Strings(refs)
	out := make([]EntityView, 0, len(refs))
	for _, ref := range refs {
		if view, ok := g.Entity(ref); ok {
			out = append(out, view)
		}
	}
	return out, nil
}

// ListRelations 返回全部关系视图，按引用排序。
func (g *Graph) ListRelations() []RelationView {
	out := make([]RelationView, 0, len(g.relations))
	for ref := range g.relations {
		out = append(out, g.snapshotRelation(ref))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Ref < out[j].Ref })
	return out
}

func splitNameKey(key string) (lang, value string, ok bool) {
	for i := 0; i < len(key); i++ {
		if key[i] == '|' {
			return key[:i], key[i+1:], true
		}
	}
	return "", "", false
}
