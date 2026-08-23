package merge

import (
	"bytes"
	"io"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// DefaultIndent is the YAML/JSON indentation used when none is given.
const DefaultIndent = 2

// Document is a merged OpenAPI document. It wraps the yaml.Node tree, which
// is what preserves key order, comments and scalar styles through the merge.
type Document struct {
	node    *yaml.Node
	version SpecVersion
}

// Node exposes the underlying tree for callers that want to inspect or
// post-process the result.
func (d *Document) Node() *yaml.Node {
	if d == nil {
		return nil
	}
	return d.node
}

// Version reports the spec version of the merged document.
func (d *Document) Version() SpecVersion {
	if d == nil {
		return SpecVersion{}
	}
	return d.version
}

// YAML renders the document as YAML.
func (d *Document) YAML(indent int) ([]byte, error) {
	var buf bytes.Buffer
	if err := d.WriteYAML(&buf, indent); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// WriteYAML renders the document as YAML into w.
func (d *Document) WriteYAML(w io.Writer, indent int) error {
	if indent <= 0 {
		indent = DefaultIndent
	}
	enc := yaml.NewEncoder(w)
	enc.SetIndent(indent)
	if err := enc.Encode(d.Node()); err != nil {
		_ = enc.Close()
		return err
	}
	return enc.Close()
}

// JSON renders the document as JSON, preserving key order.
func (d *Document) JSON(indent int) ([]byte, error) {
	return yamlx.ToJSON(d.Node(), indent)
}

// WriteJSON renders the document as JSON into w.
func (d *Document) WriteJSON(w io.Writer, indent int) error {
	data, err := d.JSON(indent)
	if err != nil {
		return err
	}
	_, err = w.Write(data)
	return err
}

// Decode unmarshals the document into a Go value, for callers that have their
// own OpenAPI structs.
func (d *Document) Decode(v any) error {
	node := d.Node()
	if node == nil {
		return ErrNoInput
	}
	return node.Decode(v)
}
