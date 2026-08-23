package merge

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// MaxInputSize caps a single input document, so a hostile or accidental
// stream cannot exhaust memory before parsing even starts.
const MaxInputSize = 256 << 20

// Source is one input document. Exactly one of Reader or Path supplies the
// bytes; FS, when set, is where Path is read from.
type Source struct {
	// Name labels the source in diagnostics. Defaults to Path.
	Name string
	// Path is read from FS, or from the OS filesystem when FS is nil.
	Path string
	// Reader, when non-nil, supplies the bytes instead of Path.
	Reader io.Reader
	// FS optionally roots Path, which makes embed.FS inputs work.
	FS fs.FS
}

// FileSource reads a document from the filesystem.
func FileSource(path string) Source { return Source{Path: path} }

// BytesSource reads a document from memory.
func BytesSource(name string, data []byte) Source {
	return Source{Name: name, Reader: bytes.NewReader(data)}
}

// FileSources builds a Source per path, for the common case of merging files:
//
//	merge.Merge(ctx, opts, merge.FileSources("a.yaml", "b.yaml")...)
func FileSources(paths ...string) []Source {
	srcs := make([]Source, len(paths))
	for i, p := range paths {
		srcs[i] = FileSource(p)
	}
	return srcs
}

// ReaderSource reads a document from a stream.
func ReaderSource(name string, r io.Reader) Source {
	return Source{Name: name, Reader: r}
}

// FSSource reads a document from an fs.FS, such as an embed.FS.
func FSSource(fsys fs.FS, path string) Source {
	return Source{Path: path, FS: fsys}
}

// Label is the name used for this source in diagnostics.
func (s Source) Label() string {
	switch {
	case s.Name != "":
		return s.Name
	case s.Path != "":
		return s.Path
	default:
		return "<input>"
	}
}

func (s Source) read(ctx context.Context) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	switch {
	case s.Reader != nil:
		data, err := io.ReadAll(io.LimitReader(s.Reader, MaxInputSize+1))
		if err != nil {
			return nil, err
		}
		if len(data) > MaxInputSize {
			return nil, fmt.Errorf("input exceeds %d bytes", MaxInputSize)
		}
		return data, nil
	case s.Path != "" && s.FS != nil:
		return fs.ReadFile(s.FS, s.Path)
	case s.Path != "":
		return os.ReadFile(s.Path) //nolint:gosec // reading a user-named input is the point
	default:
		return nil, fmt.Errorf("%w: source %q has neither Path nor Reader", ErrInvalidDocument, s.Label())
	}
}

// loadDocument reads, parses and normalises one source. A nil node with a nil
// error means the document was empty and empties are allowed.
func loadDocument(ctx context.Context, src Source, opts *Options) (*yaml.Node, error) {
	label := src.Label()

	data, err := src.read(ctx)
	if err != nil {
		return nil, newError("read", err, Diagnostic{
			Code:    CodeEmptyDocument,
			Message: fmt.Sprintf("cannot read %s", label),
			At:      Location{Source: label},
		})
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, newError("parse", fmt.Errorf("%w: %w", ErrInvalidDocument, err), Diagnostic{
			Code:    CodeInvalidDocumentCode,
			Message: fmt.Sprintf("cannot parse %s as YAML", label),
			At:      Location{Source: label},
		})
	}

	root := yamlx.Unwrap(&doc)
	if root == nil || yamlx.IsNull(root) {
		if opts.AllowEmptyDocuments {
			return nil, nil
		}
		return nil, newError("parse", ErrInvalidDocument, Diagnostic{
			Code:    CodeEmptyDocument,
			Message: fmt.Sprintf("%s is empty", label),
			At:      Location{Source: label},
		})
	}

	if !yamlx.IsMapping(root) {
		return nil, newError("parse", ErrInvalidDocument, Diagnostic{
			Code:    CodeInvalidDocumentCode,
			Message: fmt.Sprintf("%s must be a mapping at the top level, got %s", label, kindName(root)),
			At:      nodeLocation(label, root),
		})
	}

	if err := yamlx.ExpandAliases(root, opts.AliasBudget); err != nil {
		return nil, newError("expand", fmt.Errorf("%w: %w", ErrInvalidDocument, err), Diagnostic{
			Code:    CodeInvalidDocumentCode,
			Message: fmt.Sprintf("cannot expand YAML anchors in %s", label),
			At:      Location{Source: label},
		})
	}

	return root, nil
}

// CodeInvalidDocumentCode is the diagnostic code for unusable input.
const CodeInvalidDocumentCode = "invalid-document"

func kindName(n *yaml.Node) string {
	switch {
	case n == nil:
		return "nothing"
	case yamlx.IsMapping(n):
		return "a mapping"
	case yamlx.IsSequence(n):
		return "a sequence"
	case yamlx.IsScalar(n):
		return "a scalar"
	default:
		return "an unknown node"
	}
}

func nodeLocation(source string, n *yaml.Node) Location {
	if n == nil {
		return Location{Source: source}
	}
	return Location{Source: source, Line: n.Line, Column: n.Column}
}
