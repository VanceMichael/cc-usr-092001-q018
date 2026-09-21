package event

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// CanonicalDigest 对任意 JSON 字节计算稳定的 sha256 摘要。
// 为消除 map 键序与空白差异，先解码再按键排序重新编码；数字字面量通过 UseNumber 原样保留。
func CanonicalDigest(raw []byte) (string, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return "", err
	}
	canonical, err := json.Marshal(canonicalize(v))
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// canonicalize 递归返回键序确定的规范表示：map 转为有序结构。
func canonicalize(v any) any {
	switch t := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		out := make(orderedObject, 0, len(keys))
		for _, k := range keys {
			out = append(out, [2]any{k, canonicalize(t[k])})
		}
		return out
	case []any:
		for i := range t {
			t[i] = canonicalize(t[i])
		}
		return t
	default:
		return v
	}
}

// orderedObject 借助 json.Marshaler 输出键序确定的 JSON 对象。
type orderedObject [][2]any

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var b []byte
	b = append(b, '{')
	for i, kv := range o {
		if i > 0 {
			b = append(b, ',')
		}
		key, err := json.Marshal(kv[0])
		if err != nil {
			return nil, err
		}
		b = append(b, key...)
		b = append(b, ':')
		val, err := json.Marshal(kv[1])
		if err != nil {
			return nil, err
		}
		b = append(b, val...)
	}
	b = append(b, '}')
	return b, nil
}
