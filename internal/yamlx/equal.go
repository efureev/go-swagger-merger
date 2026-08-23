package yamlx

import (
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// Equal reports semantic equality of two nodes.
//
// This is what keeps the strict default usable: a shared schema copied into
// several input files must dedupe silently rather than raise a conflict. So
// mappings compare as unordered key/value sets, and comments, anchors and
// scalar styles are ignored. Sequences stay order-sensitive because order is
// meaningful in OpenAPI (servers, parameters, enum).
func Equal(a, b *yaml.Node) bool {
	return equal(Unwrap(a), Unwrap(b), 0)
}

const maxEqualDepth = 512

func equal(a, b *yaml.Node, depth int) bool {
	if depth > maxEqualDepth {
		return false
	}
	if a == nil || b == nil {
		return IsNull(a) && IsNull(b)
	}
	if a.Kind == yaml.AliasNode {
		return equal(a.Alias, b, depth+1)
	}
	if b.Kind == yaml.AliasNode {
		return equal(a, b.Alias, depth+1)
	}
	if a.Kind != b.Kind || Tag(a) != Tag(b) {
		return false
	}

	switch a.Kind {
	case yaml.ScalarNode:
		return scalarEqual(a, b)
	case yaml.SequenceNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !equal(a.Content[i], b.Content[i], depth+1) {
				return false
			}
		}
		return true
	case yaml.MappingNode:
		ae, be := Entries(a), Entries(b)
		if len(ae) != len(be) || len(ae)*2 != len(a.Content) || len(be)*2 != len(b.Content) {
			return false
		}
		for _, e := range ae {
			bv, ok := MapValue(b, e.Key)
			if !ok || !equal(e.Value, bv, depth+1) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// scalarEqual compares scalars by value, normalising numeric spellings so that
// 1.0 and 1.00 (both !!float) compare equal while 1 (!!int) stays distinct.
func scalarEqual(a, b *yaml.Node) bool {
	if a.Value == b.Value {
		return true
	}
	switch Tag(a) {
	case TagFloat:
		af, aerr := strconv.ParseFloat(a.Value, 64)
		bf, berr := strconv.ParseFloat(b.Value, 64)
		return aerr == nil && berr == nil && af == bf
	case TagInt:
		ai, aerr := strconv.ParseInt(a.Value, 0, 64)
		bi, berr := strconv.ParseInt(b.Value, 0, 64)
		return aerr == nil && berr == nil && ai == bi
	case TagBool:
		return strings.EqualFold(a.Value, b.Value)
	case TagNull:
		return true
	default:
		return false
	}
}

// Canonical renders a node into a stable string, mappings sorted by key. It is
// the dedup key for values that have no natural identifier, such as security
// requirement objects.
func Canonical(n *yaml.Node) string {
	var sb strings.Builder
	canonical(&sb, Unwrap(n), 0)
	return sb.String()
}

func canonical(sb *strings.Builder, n *yaml.Node, depth int) {
	if depth > maxEqualDepth {
		sb.WriteString("!truncated")
		return
	}
	if n == nil {
		sb.WriteString("~")
		return
	}
	if n.Kind == yaml.AliasNode {
		canonical(sb, n.Alias, depth+1)
		return
	}
	switch n.Kind {
	case yaml.ScalarNode:
		sb.WriteString(Tag(n))
		sb.WriteByte(':')
		sb.WriteString(strconv.Quote(n.Value))
	case yaml.SequenceNode:
		sb.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				sb.WriteByte(',')
			}
			canonical(sb, c, depth+1)
		}
		sb.WriteByte(']')
	case yaml.MappingNode:
		entries := Entries(n)
		sort.SliceStable(entries, func(i, j int) bool { return entries[i].Key < entries[j].Key })
		sb.WriteByte('{')
		for i, e := range entries {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(strconv.Quote(e.Key))
			sb.WriteByte(':')
			canonical(sb, e.Value, depth+1)
		}
		sb.WriteByte('}')
	default:
		sb.WriteString("~")
	}
}
