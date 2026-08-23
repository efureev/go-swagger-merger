package yamlx

import (
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

// Pointer joins tokens into an RFC 6901 JSON pointer.
func Pointer(tokens ...string) string {
	if len(tokens) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, t := range tokens {
		sb.WriteByte('/')
		sb.WriteString(EscapeToken(t))
	}
	return sb.String()
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

// Walk visits every node in the tree, passing the RFC 6901 pointer that
// locates it. Returning an error from fn aborts the walk. Alias nodes are
// visited but not followed, so a cyclic document terminates.
func Walk(root *yaml.Node, fn func(pointer string, n *yaml.Node) error) error {
	return walk(Unwrap(root), "", fn)
}

func walk(n *yaml.Node, pointer string, fn func(string, *yaml.Node) error) error {
	if n == nil {
		return nil
	}
	if err := fn(pointer, n); err != nil {
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
