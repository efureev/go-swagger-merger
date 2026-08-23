package yamlx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ToJSON encodes a node tree as JSON, emitting object members in document
// order. Marshalling through map[string]any instead would reintroduce Go's
// randomised map iteration and make output non-deterministic, which is exactly
// the defect this rewrite exists to remove. indent <= 0 emits compact JSON.
func ToJSON(n *yaml.Node, indent int) ([]byte, error) {
	var buf bytes.Buffer
	if err := writeJSON(&buf, Unwrap(n), indent, 0); err != nil {
		return nil, err
	}
	buf.WriteByte('\n')
	return buf.Bytes(), nil
}

func writeJSON(buf *bytes.Buffer, n *yaml.Node, indent, depth int) error {
	if n == nil {
		buf.WriteString("null")
		return nil
	}
	if n.Kind == yaml.AliasNode {
		return writeJSON(buf, n.Alias, indent, depth)
	}

	switch n.Kind {
	case yaml.ScalarNode:
		buf.WriteString(jsonScalar(n))
		return nil

	case yaml.SequenceNode:
		if len(n.Content) == 0 {
			buf.WriteString("[]")
			return nil
		}
		buf.WriteByte('[')
		for i, c := range n.Content {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeNewlineIndent(buf, indent, depth+1)
			if err := writeJSON(buf, c, indent, depth+1); err != nil {
				return err
			}
		}
		writeNewlineIndent(buf, indent, depth)
		buf.WriteByte(']')
		return nil

	case yaml.MappingNode:
		if len(n.Content) == 0 {
			buf.WriteString("{}")
			return nil
		}
		buf.WriteByte('{')
		for i := 0; i+1 < len(n.Content); i += 2 {
			if i > 0 {
				buf.WriteByte(',')
			}
			writeNewlineIndent(buf, indent, depth+1)
			buf.WriteString(quoteJSON(jsonKey(n.Content[i])))
			buf.WriteByte(':')
			if indent > 0 {
				buf.WriteByte(' ')
			}
			if err := writeJSON(buf, n.Content[i+1], indent, depth+1); err != nil {
				return err
			}
		}
		writeNewlineIndent(buf, indent, depth)
		buf.WriteByte('}')
		return nil

	default:
		return fmt.Errorf("cannot encode yaml node kind %d as JSON", n.Kind)
	}
}

func writeNewlineIndent(buf *bytes.Buffer, indent, depth int) {
	if indent <= 0 {
		return
	}
	buf.WriteByte('\n')
	buf.WriteString(strings.Repeat(" ", indent*depth))
}

// jsonKey renders a mapping key. OpenAPI keys are always strings; anything
// else is rendered canonically rather than dropped.
func jsonKey(n *yaml.Node) string {
	if IsScalar(n) {
		return n.Value
	}
	return Canonical(n)
}

func jsonScalar(n *yaml.Node) string {
	switch Tag(n) {
	case TagNull:
		return "null"
	case TagBool:
		if strings.EqualFold(n.Value, "true") || n.Value == "y" || n.Value == "yes" || n.Value == "on" {
			return "true"
		}
		return "false"
	case TagInt:
		// Normalises YAML's 0x/0o/0b spellings into JSON numbers.
		if i, err := strconv.ParseInt(n.Value, 0, 64); err == nil {
			return strconv.FormatInt(i, 10)
		}
		if u, err := strconv.ParseUint(n.Value, 0, 64); err == nil {
			return strconv.FormatUint(u, 10)
		}
		return quoteJSON(n.Value)
	case TagFloat:
		f, err := strconv.ParseFloat(n.Value, 64)
		if err != nil {
			// .inf and .nan have no JSON spelling; keep them as strings.
			return quoteJSON(n.Value)
		}
		return strconv.FormatFloat(f, 'g', -1, 64)
	default:
		return quoteJSON(n.Value)
	}
}

// quoteJSON escapes a string without Go's default HTML escaping, so that
// descriptions containing <, > or & stay readable in the output.
func quoteJSON(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return strconv.Quote(s)
	}
	return strings.TrimSuffix(buf.String(), "\n")
}
