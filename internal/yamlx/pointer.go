package yamlx

import (
	"errors"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// EscapeToken encodes one RFC 6901 reference token.
func EscapeToken(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	return strings.ReplaceAll(s, "/", "~1")
}

// UnescapeToken decodes one RFC 6901 reference token.
func UnescapeToken(s string) string {
	s = strings.ReplaceAll(s, "~1", "/")
	return strings.ReplaceAll(s, "~0", "~")
}

// At resolves an RFC 6901 pointer against root. The empty pointer selects the
// root itself.
func At(root *yaml.Node, pointer string) (*yaml.Node, bool) {
	cur := Unwrap(root)
	if pointer == "" || pointer == "#" {
		return cur, cur != nil
	}
	pointer = strings.TrimPrefix(pointer, "#")
	if !strings.HasPrefix(pointer, "/") {
		return nil, false
	}
	for _, raw := range strings.Split(pointer[1:], "/") {
		token := UnescapeToken(raw)
		if cur != nil && cur.Kind == yaml.AliasNode {
			cur = cur.Alias
		}
		switch {
		case IsMapping(cur):
			v, ok := MapValue(cur, token)
			if !ok {
				return nil, false
			}
			cur = v
		case IsSequence(cur):
			idx, err := strconv.Atoi(token)
			if err != nil || idx < 0 || idx >= len(cur.Content) {
				return nil, false
			}
			cur = cur.Content[idx]
		default:
			return nil, false
		}
	}
	return cur, cur != nil
}

// SkipSubtree tells Walk not to descend into the node just visited. Returning
// it from the callback is not an error.
//
// The name deliberately omits the Err prefix, matching fs.SkipDir and
// fs.SkipAll: this is a control-flow signal, and calling it ErrSkipSubtree
// would suggest something went wrong.
//
//nolint:staticcheck,revive // ST1012: see above
var SkipSubtree = errors.New("skip this subtree")

// Walk visits every node in the tree, passing the RFC 6901 pointer that
// locates it. Returning an error from fn aborts the walk; returning
// [SkipSubtree] prunes the node's children and continues. Alias nodes are
// visited but not followed, so a cyclic document terminates.
func Walk(root *yaml.Node, fn func(pointer string, n *yaml.Node) error) error {
	err := walk(Unwrap(root), "", fn)
	if errors.Is(err, SkipSubtree) {
		return nil
	}
	return err
}

func walk(n *yaml.Node, pointer string, fn func(string, *yaml.Node) error) error {
	if n == nil {
		return nil
	}
	switch err := fn(pointer, n); {
	case errors.Is(err, SkipSubtree):
		return nil
	case err != nil:
		return err
	}
	switch n.Kind {
	case yaml.MappingNode:
		for _, e := range Entries(n) {
			if err := walk(e.Value, pointer+"/"+EscapeToken(e.Key), fn); err != nil {
				return err
			}
		}
	case yaml.SequenceNode:
		for i, c := range n.Content {
			if err := walk(c, pointer+"/"+strconv.Itoa(i), fn); err != nil {
				return err
			}
		}
	}
	return nil
}
