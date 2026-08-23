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

// refOpaqueFields hold arbitrary user data rather than OpenAPI objects. A
// "$ref" key inside one of them is a literal string in a payload sample, not a
// reference, so the walk must not descend into them: a schema whose example
// happens to document a $ref-shaped body is perfectly valid and must not fail
// the merge.
var refOpaqueFields = map[string]bool{
	"example": true,
	"default": true,
	"enum":    true,
	"const":   true,
}

// nameMapFields are the objects whose keys are chosen by the spec author. The
// member below such a key is a definition, so its name must never be read as a
// spec field -- a schema called "example" is not an example, and a property
// called "$ref" is not a reference.
var nameMapFields = map[string]bool{
	"schemas": true, "responses": true, "parameters": true, "examples": true,
	"requestBodies": true, "headers": true, "securitySchemes": true,
	"links": true, "callbacks": true, "pathItems": true,
	"definitions": true, "securityDefinitions": true,
	"properties": true, "patternProperties": true,
	"paths": true, "webhooks": true, "content": true, "encoding": true,
	"variables": true, "mapping": true, "scopes": true,
}

func pointerTokens(pointer string) []string {
	if pointer == "" {
		return nil
	}
	raw := strings.Split(strings.TrimPrefix(pointer, "/"), "/")
	out := make([]string, len(raw))
	for i, t := range raw {
		out[i] = yamlx.UnescapeToken(t)
	}
	return out
}

// opaqueForRefs reports whether a subtree holds data rather than spec objects.
func opaqueForRefs(pointer string) bool {
	tokens := pointerTokens(pointer)
	n := len(tokens)
	if n == 0 {
		return false
	}
	// Directly under a name map this token is an author-chosen name.
	if n >= 2 && nameMapFields[tokens[n-2]] {
		return false
	}
	if refOpaqueFields[tokens[n-1]] {
		return true
	}
	// An Example Object may itself be a $ref, but its "value" is the payload.
	return tokens[n-1] == "value" && n >= 3 && tokens[n-3] == "examples"
}

// referenceTarget reports the $ref value node when n is a Reference Object.
//
// Requiring a scalar $ref and refusing to read a name map as one keeps
// author-chosen keys out of the reference space.
func referenceTarget(pointer string, n *yaml.Node) (*yaml.Node, bool) {
	if !yamlx.IsMapping(n) {
		return nil, false
	}
	_, val, ok := yamlx.MapGet(n, "$ref")
	if !ok || !yamlx.IsScalar(val) {
		return nil, false
	}
	if tokens := pointerTokens(pointer); len(tokens) > 0 && nameMapFields[tokens[len(tokens)-1]] {
		return nil, false
	}
	return val, true
}

// refScan is the outcome of the single pass over the merged document.
type refScan struct {
	refs     []Ref
	siblings []Diagnostic
}

// scanRefs walks the merged document once, gathering references and the keys
// placed beside them. One walk serves both the dangling-reference check and
// the unused-component report, which are independently switchable.
func (m *Merger) scanRefs() refScan {
	allowedSiblings := map[string]bool{"$ref": true}
	if m.version.Family == FamilyOpenAPI3 && m.version.Minor >= 1 {
		allowedSiblings["summary"] = true
		allowedSiblings["description"] = true
	}

	var scan refScan
	_ = yamlx.Walk(m.root, func(pointer string, n *yaml.Node) error {
		if opaqueForRefs(pointer) {
			return yamlx.SkipSubtree
		}
		val, ok := referenceTarget(pointer, n)
		if !ok {
			return nil
		}

		// The reference itself is the actionable location, not its container.
		scan.refs = append(scan.refs, Ref{
			Pointer: pointer,
			Target:  val.Value,
			Kind:    classifyRef(val.Value),
			At:      m.locateInResult(pointer, val),
		})

		var extra []string
		for _, e := range yamlx.Entries(n) {
			if !allowedSiblings[e.Key] {
				extra = append(extra, e.Key)
			}
		}
		if len(extra) > 0 {
			scan.siblings = append(scan.siblings, Diagnostic{
				Code:    CodeRefSiblings,
				Message: fmt.Sprintf("keys next to $ref are ignored by this spec version: %s", strings.Join(extra, ", ")),
				At:      m.locateInResult(pointer, n),
				Pointer: pointer,
			})
		}
		return nil
	})
	return scan
}

// locateInResult recovers the input file for a node in the merged tree.
//
// Cloned nodes keep their line and column but not their filename, so the
// filename comes from the nearest enclosing pointer that was recorded when the
// definition was installed.
func (m *Merger) locateInResult(pointer string, n *yaml.Node) Location {
	loc := Location{}
	if n != nil {
		loc.Line, loc.Column = n.Line, n.Column
	}
	for p := pointer; ; {
		if origin, ok := m.origin[p]; ok {
			loc.Source = origin.Source
			if loc.Line == 0 {
				loc.Line, loc.Column = origin.Line, origin.Column
			}
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
func (m *Merger) validateRefs(refs []Ref, siblings []Diagnostic) error {
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
	for _, d := range siblings {
		m.warn(d)
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

// componentContainers lists the pointers whose members are addressable
// definitions, per spec family.
func (m *Merger) componentContainers() []string {
	if m.version.Family == FamilySwagger2 {
		return []string{"/definitions", "/parameters", "/responses", "/securityDefinitions"}
	}
	components, ok := yamlx.MapValue(m.root, "components")
	if !ok || !yamlx.IsMapping(components) {
		return nil
	}
	entries := yamlx.Entries(components)
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		out = append(out, "/components/"+yamlx.EscapeToken(e.Key))
	}
	return out
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
