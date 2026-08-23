package merge

import (
	"fmt"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// RefKind classifies where a $ref points.
type RefKind uint8

// Reference kinds.
const (
	// RefLocal points inside the same document, e.g. #/components/schemas/User.
	RefLocal RefKind = iota
	// RefFile points at another file, e.g. ./common.yaml#/components/schemas/Error.
	RefFile
	// RefRemote points at a URL.
	RefRemote
)

// Ref is one $ref occurrence in the merged document.
type Ref struct {
	// Pointer locates the object holding the $ref.
	Pointer string
	// Target is the raw reference string.
	Target string
	Kind   RefKind
	At     Location
}

func classifyRef(target string) RefKind {
	switch {
	case strings.HasPrefix(target, "#"):
		return RefLocal
	case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"),
		strings.HasPrefix(target, "//"):
		return RefRemote
	default:
		return RefFile
	}
}

// collectRefs walks the merged document and gathers every $ref, attributing
// each to the input file it came from.
func (m *Merger) collectRefs() []Ref {
	var refs []Ref
	_ = yamlx.Walk(m.root, func(pointer string, n *yaml.Node) error {
		if !yamlx.IsMapping(n) {
			return nil
		}
		_, val, ok := yamlx.MapGet(n, "$ref")
		if !ok || !yamlx.IsScalar(val) {
			return nil
		}
		refs = append(refs, Ref{
			Pointer: pointer,
			Target:  val.Value,
			Kind:    classifyRef(val.Value),
			At:      m.locateInResult(pointer, val),
		})
		return nil
	})
	return refs
}

// locateInResult recovers the input file for a node in the merged tree.
//
// Cloned nodes keep their line and column but not their filename, so the
// filename comes from the nearest enclosing pointer that was recorded when the
// definition was installed.
func (m *Merger) locateInResult(pointer string, n *yaml.Node) Location {
	loc := Location{Line: n.Line, Column: n.Column}
	for p := pointer; ; {
		if origin, ok := m.origin[p]; ok {
			loc.Source = origin.Source
			return loc
		}
		cut := strings.LastIndexByte(p, '/')
		if cut < 0 {
			return loc
		}
		p = p[:cut]
	}
}

// validateRefs checks that every local $ref resolves inside the merged
// document. Merging is exactly where a reference goes stale: a file that was
// self-consistent on its own can lose its target to a conflict policy.
func (m *Merger) validateRefs() error {
	refs := m.collectRefs()

	for _, ref := range refs {
		switch ref.Kind {
		case RefLocal:
			if !m.resolvesLocally(ref.Target) {
				return newError("refs", ErrDanglingRef, Diagnostic{
					Code:    CodeDanglingRef,
					Message: fmt.Sprintf("$ref %q does not resolve in the merged document", ref.Target),
					At:      ref.At,
					Pointer: ref.Pointer,
				})
			}
		case RefFile, RefRemote:
			m.warn(Diagnostic{
				Code:    CodeExternalRef,
				Message: fmt.Sprintf("$ref %q points outside the document and was left as-is", ref.Target),
				At:      ref.At,
				Pointer: ref.Pointer,
			})
		}
	}

	m.checkRefSiblings()
	if m.opts.ReportUnusedComponents {
		m.reportUnusedComponents(refs)
	}
	return nil
}

// resolvesLocally reports whether a local reference finds its target. The
// fragment may be percent-encoded, which is legal and which real specifications
// do use for path templates, so a failed lookup is retried decoded before the
// reference is called dangling.
func (m *Merger) resolvesLocally(target string) bool {
	if _, ok := yamlx.At(m.root, target); ok {
		return true
	}
	decoded, err := url.PathUnescape(target)
	if err != nil || decoded == target {
		return false
	}
	_, ok := yamlx.At(m.root, decoded)
	return ok
}

// checkRefSiblings reports keys placed next to a $ref. OpenAPI 3.0 ignores
// them outright; 3.1 honours only summary and description.
func (m *Merger) checkRefSiblings() {
	allowed := map[string]bool{"$ref": true}
	if m.version.Family == FamilyOpenAPI3 && m.version.Minor >= 1 {
		allowed["summary"] = true
		allowed["description"] = true
	}

	_ = yamlx.Walk(m.root, func(pointer string, n *yaml.Node) error {
		if !yamlx.IsMapping(n) {
			return nil
		}
		if _, _, ok := yamlx.MapGet(n, "$ref"); !ok {
			return nil
		}
		var extra []string
		for _, e := range yamlx.Entries(n) {
			if !allowed[e.Key] {
				extra = append(extra, e.Key)
			}
		}
		if len(extra) == 0 {
			return nil
		}
		m.warn(Diagnostic{
			Code:    CodeRefSiblings,
			Message: fmt.Sprintf("keys next to $ref are ignored by this spec version: %s", strings.Join(extra, ", ")),
			At:      m.locateInResult(pointer, n),
			Pointer: pointer,
		})
		return nil
	})
}

// componentContainers lists the pointers whose members are addressable
// definitions, per spec family.
func (m *Merger) componentContainers() []string {
	if m.version.Family == FamilySwagger2 {
		return []string{"/definitions", "/parameters", "/responses", "/securityDefinitions"}
	}
	out := make([]string, 0, len(componentSections))
	for _, e := range yamlx.Entries(mustMapping(m.root, "components")) {
		out = append(out, "/components/"+yamlx.EscapeToken(e.Key))
	}
	return out
}

func mustMapping(root *yaml.Node, key string) *yaml.Node {
	v, ok := yamlx.MapValue(root, key)
	if !ok || !yamlx.IsMapping(v) {
		return nil
	}
	return v
}

// reportUnusedComponents notes definitions nothing references. Merging tends
// to accumulate them, and they are the first thing to drop when trimming a
// combined spec.
func (m *Merger) reportUnusedComponents(refs []Ref) {
	referenced := make(map[string]bool, len(refs))
	for _, ref := range refs {
		if ref.Kind != RefLocal {
			continue
		}
		ptr := strings.TrimPrefix(ref.Target, "#")
		referenced[ptr] = true
		if decoded, err := url.PathUnescape(ptr); err == nil {
			referenced[decoded] = true
		}
	}

	for _, container := range m.componentContainers() {
		node, ok := yamlx.At(m.root, container)
		if !ok || !yamlx.IsMapping(node) {
			continue
		}
		for _, e := range yamlx.Entries(node) {
			ptr := container + "/" + yamlx.EscapeToken(e.Key)
			if referenced[ptr] {
				continue
			}
			m.warn(Diagnostic{
				Code:    CodeUnusedComponent,
				Message: fmt.Sprintf("%s is not referenced by any $ref", ptr),
				At:      m.locateInResult(ptr, e.Value),
				Pointer: ptr,
			})
		}
	}
}
