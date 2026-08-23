// Package yamlx provides low-level helpers for manipulating yaml.Node trees.
//
// The merger operates on yaml.Node rather than map[string]any so that key
// order, comments and scalar styles survive a merge. Every helper here obeys
// one rule: output order is derived from Node.Content slices only, never from
// Go map iteration.
package yamlx

import (
	"sort"

	"gopkg.in/yaml.v3"
)

// Tags used when constructing nodes from scratch.
const (
	TagMap   = "!!map"
	TagSeq   = "!!seq"
	TagStr   = "!!str"
	TagInt   = "!!int"
	TagFloat = "!!float"
	TagBool  = "!!bool"
	TagNull  = "!!null"
)

// NewMapping returns an empty mapping node.
func NewMapping() *yaml.Node {
	return &yaml.Node{Kind: yaml.MappingNode, Tag: TagMap}
}

// NewSequence returns an empty sequence node.
func NewSequence() *yaml.Node {
	return &yaml.Node{Kind: yaml.SequenceNode, Tag: TagSeq}
}

// NewScalar returns a string scalar node.
func NewScalar(value string) *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: TagStr, Value: value}
}

// Unwrap descends through document nodes to the value they carry. It returns
// nil for an empty document.
func Unwrap(n *yaml.Node) *yaml.Node {
	for n != nil && n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		n = n.Content[0]
	}
	return n
}

// Tag reports the node's tag, inferring one when the node was built by hand.
func Tag(n *yaml.Node) string {
	if n == nil {
		return TagNull
	}
	if n.Tag != "" {
		return n.Tag
	}
	switch n.Kind {
	case yaml.MappingNode:
		return TagMap
	case yaml.SequenceNode:
		return TagSeq
	default:
		return TagStr
	}
}

// IsMapping reports whether n is a mapping node.
func IsMapping(n *yaml.Node) bool { return n != nil && n.Kind == yaml.MappingNode }

// IsSequence reports whether n is a sequence node.
func IsSequence(n *yaml.Node) bool { return n != nil && n.Kind == yaml.SequenceNode }

// IsScalar reports whether n is a scalar node.
func IsScalar(n *yaml.Node) bool { return n != nil && n.Kind == yaml.ScalarNode }

// IsNull reports whether n is absent, the YAML null scalar, or the zero node
// that an empty document parses into.
func IsNull(n *yaml.Node) bool {
	if n == nil || n.Kind == 0 {
		return true
	}
	return n.Kind == yaml.ScalarNode && Tag(n) == TagNull
}

// scalarKey returns the key string of a mapping key node, and whether the node
// is usable as a string key at all.
func scalarKey(n *yaml.Node) (string, bool) {
	if n == nil || n.Kind != yaml.ScalarNode {
		return "", false
	}
	return n.Value, true
}

// MapGet looks up key in a mapping node, returning both the key node (which
// carries comments) and the value node.
func MapGet(n *yaml.Node, key string) (kn, vn *yaml.Node, ok bool) {
	if !IsMapping(n) {
		return nil, nil, false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if k, isStr := scalarKey(n.Content[i]); isStr && k == key {
			return n.Content[i], n.Content[i+1], true
		}
	}
	return nil, nil, false
}

// MapValue is MapGet reduced to the value node.
func MapValue(n *yaml.Node, key string) (*yaml.Node, bool) {
	_, v, ok := MapGet(n, key)
	return v, ok
}

// MapString returns the value of key when it is a scalar.
func MapString(n *yaml.Node, key string) (string, bool) {
	v, ok := MapValue(n, key)
	if !ok || !IsScalar(v) {
		return "", false
	}
	return v.Value, true
}

// MapSet assigns key to val, replacing an existing entry in place (preserving
// its position and key comments) or appending a new one at the end.
func MapSet(n *yaml.Node, key string, val *yaml.Node) {
	MapSetNode(n, NewScalar(key), val)
}

// MapSetNode is MapSet with an explicit key node, used when the key's comments
// should travel with it.
func MapSetNode(n *yaml.Node, keyNode, val *yaml.Node) {
	if !IsMapping(n) {
		return
	}
	key, ok := scalarKey(keyNode)
	if ok {
		for i := 0; i+1 < len(n.Content); i += 2 {
			if k, isStr := scalarKey(n.Content[i]); isStr && k == key {
				n.Content[i+1] = val
				return
			}
		}
	}
	n.Content = append(n.Content, keyNode, val)
}

// MapDelete removes key, reporting whether it was present.
func MapDelete(n *yaml.Node, key string) bool {
	if !IsMapping(n) {
		return false
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if k, isStr := scalarKey(n.Content[i]); isStr && k == key {
			n.Content = append(n.Content[:i:i], n.Content[i+2:]...)
			return true
		}
	}
	return false
}

// MapKeys lists the mapping's keys in document order.
func MapKeys(n *yaml.Node) []string {
	if !IsMapping(n) {
		return nil
	}
	keys := make([]string, 0, len(n.Content)/2)
	for i := 0; i < len(n.Content); i += 2 {
		if k, ok := scalarKey(n.Content[i]); ok {
			keys = append(keys, k)
		}
	}
	return keys
}

// MapEntry is one key/value pair of a mapping, in document order.
type MapEntry struct {
	Key   string
	KeyN  *yaml.Node
	Value *yaml.Node
}

// Entries lists a mapping's pairs in document order. Iterating the result is
// the only sanctioned way to walk a mapping when producing output: it keeps
// results deterministic where ranging over a Go map would not.
func Entries(n *yaml.Node) []MapEntry {
	if !IsMapping(n) {
		return nil
	}
	out := make([]MapEntry, 0, len(n.Content)/2)
	for i := 0; i+1 < len(n.Content); i += 2 {
		k, ok := scalarKey(n.Content[i])
		if !ok {
			continue
		}
		out = append(out, MapEntry{Key: k, KeyN: n.Content[i], Value: n.Content[i+1]})
	}
	return out
}

// Clone deep-copies a node. Merged documents must never alias nodes owned by
// an input document, or a later edit to one would silently mutate the other.
func Clone(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	cp := *n
	cp.Content = nil
	if len(n.Content) > 0 {
		cp.Content = make([]*yaml.Node, len(n.Content))
		for i, c := range n.Content {
			cp.Content[i] = Clone(c)
		}
	}
	// An alias in the copy would point back into the source tree.
	cp.Alias = nil
	return &cp
}

// SortMapping reorders a mapping's pairs by less over their keys. Pairs whose
// key is not a scalar keep their relative position at the end.
func SortMapping(n *yaml.Node, less func(a, b string) bool) {
	if !IsMapping(n) || len(n.Content) < 4 {
		return
	}
	entries := Entries(n)
	if len(entries)*2 != len(n.Content) {
		return // non-scalar keys present; reordering would drop pairs
	}
	sort.SliceStable(entries, func(i, j int) bool { return less(entries[i].Key, entries[j].Key) })
	content := make([]*yaml.Node, 0, len(n.Content))
	for _, e := range entries {
		content = append(content, e.KeyN, e.Value)
	}
	n.Content = content
}
