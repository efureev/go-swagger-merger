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

// fieldRoles reports, token by token, whether a pointer token is a spec field
// rather than a key the author chose. A key is author-chosen when it sits
// directly in a field that holds a name map, and the members of whatever it
// names are fields again. Deciding from the whole pointer, rather than from
// the last token or two, is what tells a property called "content" from a
// media type map.
func fieldRoles(tokens []string) []bool {
	field := make([]bool, len(tokens))
	for i := range tokens {
		field[i] = i == 0 || !field[i-1] || !nameMapFields[tokens[i-1]]
	}
	return field
}

// opaqueForRefs reports whether a subtree holds data rather than spec objects.
func opaqueForRefs(tokens []string, field []bool, n *yaml.Node, family Family) bool {
	last := len(tokens) - 1
	if last < 0 || !field[last] {
		return false
	}
	switch {
	case refOpaqueFields[tokens[last]]:
		return true
	case tokens[last] == "examples":
		// Swagger 2.0 has no Example Object: its one examples field maps a
		// media type to a payload. A 3.1 schema's examples is a list of
		// payloads. Every other examples is a name map of Example Objects.
		return family == FamilySwagger2 || yamlx.IsSequence(n)
	case tokens[last] == "value":
		// An Example Object may itself be a $ref, but its "value" is the payload.
		return last >= 2 && !field[last-1] && tokens[last-2] == "examples"
	}
	return false
}

// isNameMap reports whether the node at a pointer is a map of author-chosen
// names, whose keys must never be read as spec fields.
func isNameMap(tokens []string, field []bool) bool {
	last := len(tokens) - 1
	return last >= 0 && field[last] && nameMapFields[tokens[last]]
}

// referenceTarget reports the $ref value node when n is a Reference Object.
//
// Requiring a scalar $ref and refusing to read a name map as one keeps
// author-chosen keys out of the reference space.
func referenceTarget(tokens []string, field []bool, n *yaml.Node) (*yaml.Node, bool) {
	if !yamlx.IsMapping(n) || isNameMap(tokens, field) {
		return nil, false
	}
	_, val, ok := yamlx.MapGet(n, "$ref")
	if !ok || !yamlx.IsScalar(val) {
		return nil, false
	}
	return val, true
}

// inSchema reports whether a pointer lies inside a Schema Object. Every
// schema-valued field leads into one, and nothing inside leads back out.
func inSchema(tokens []string, field []bool) bool {
	for i, t := range tokens {
		if field[i] && (t == "schema" || (t == "schemas" && i+1 < len(tokens))) {
			return true
		}
	}
	return false
}

// refScan is the outcome of the single pass over the merged document.
type refScan struct {
	refs     []Ref
	siblings []Diagnostic
	// anchors maps each 3.1 $anchor or $dynamicAnchor to the pointer of the
	// schema declaring it, since a plain-name fragment such as #foo names one.
	anchors map[string]string
	// schemesUsed holds the security schemes some security requirement names.
	// Requirements name schemes directly, never through a $ref.
	schemesUsed map[string]bool
}

// scanRefs walks the merged document once, gathering references and the keys
// placed beside them. One walk serves both the dangling-reference check and
// the unused-component report, which are independently switchable.
func (m *Merger) scanRefs() refScan {
	is31 := m.version.Family == FamilyOpenAPI3 && m.version.Minor >= 1
	allowedSiblings := map[string]bool{"$ref": true}
	if is31 {
		allowedSiblings["summary"] = true
		allowedSiblings["description"] = true
	}

	scan := refScan{anchors: map[string]string{}, schemesUsed: map[string]bool{}}
	_ = yamlx.Walk(m.root, func(pointer string, n *yaml.Node) error {
		tokens := pointerTokens(pointer)
		field := fieldRoles(tokens)
		if opaqueForRefs(tokens, field, n, m.version.Family) {
			return yamlx.SkipSubtree
		}
		scan.noteAnchors(pointer, tokens, field, n)
		scan.noteSecurityRequirement(tokens, field, n)

		val, ok := referenceTarget(tokens, field, n)
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

		// A 3.1 Schema Object is JSON Schema 2020-12, where every keyword
		// beside $ref applies; only the other Reference Objects ignore them.
		if is31 && inSchema(tokens, field) {
			return nil
		}
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

// noteAnchors records the anchors a schema declares, which plain-name
// fragments resolve through.
func (s *refScan) noteAnchors(pointer string, tokens []string, field []bool, n *yaml.Node) {
	if !yamlx.IsMapping(n) || isNameMap(tokens, field) {
		return
	}
	for _, key := range []string{"$anchor", "$dynamicAnchor"} {
		if name, ok := yamlx.MapString(n, key); ok && name != "" {
			s.anchors[name] = pointer
		}
	}
}

// noteSecurityRequirement records the schemes named by one element of a
// security list, at the root or on an operation.
func (s *refScan) noteSecurityRequirement(tokens []string, field []bool, n *yaml.Node) {
	last := len(tokens) - 1
	if last < 1 || !yamlx.IsMapping(n) || !field[last-1] || tokens[last-1] != "security" {
		return
	}
	for _, e := range yamlx.Entries(n) {
		s.schemesUsed[e.Key] = true
	}
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
func (m *Merger) validateRefs(scan refScan) error {
	for _, ref := range scan.refs {
		switch ref.Kind {
		case RefLocal:
			if !m.resolvesLocally(ref.Target, scan.anchors) {
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
	for _, d := range scan.siblings {
		m.warn(d)
	}
	return nil
}

// anchorName returns the name in a plain-name fragment such as #foo, which
// JSON Schema 2020-12 resolves through $anchor rather than as a pointer.
func anchorName(target string) (string, bool) {
	fragment := strings.TrimPrefix(target, "#")
	if fragment == "" || strings.HasPrefix(fragment, "/") {
		return "", false
	}
	return fragment, true
}

// resolvesLocally reports whether a local reference finds its target. The
// fragment may be percent-encoded, which is legal and which real specifications
// do use for path templates, so a failed lookup is retried decoded before the
// reference is called dangling.
func (m *Merger) resolvesLocally(target string, anchors map[string]string) bool {
	if name, ok := anchorName(target); ok {
		_, declared := anchors[name]
		return declared
	}
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
func (m *Merger) reportUnusedComponents(scan refScan) {
	referenced := make(map[string]bool, len(scan.refs))
	for _, ref := range scan.refs {
		if ref.Kind != RefLocal {
			continue
		}
		if name, ok := anchorName(ref.Target); ok {
			referenced[scan.anchors[name]] = true
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
		// Security requirements name schemes directly, never through a $ref.
		schemes := container == "/components/securitySchemes" || container == "/securityDefinitions"
		for _, e := range yamlx.Entries(node) {
			ptr := container + "/" + yamlx.EscapeToken(e.Key)
			if referenced[ptr] || (schemes && scan.schemesUsed[e.Key]) {
				continue
			}
			msg := fmt.Sprintf("%s is not referenced by any $ref", ptr)
			if schemes {
				msg = fmt.Sprintf("%s is not used by any security requirement", ptr)
			}
			m.warn(Diagnostic{
				Code:    CodeUnusedComponent,
				Message: msg,
				At:      m.locateInResult(ptr, e.Value),
				Pointer: ptr,
			})
		}
	}
}
