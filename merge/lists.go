package merge

import (
	"fmt"
	"strconv"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// identityFunc derives the deduplication key of a sequence element. natural is
// false when the element lacks the field that should identify it, which is
// worth reporting.
type identityFunc func(n *yaml.Node) (id string, natural bool)

func fieldIdentity(field string) identityFunc {
	return func(n *yaml.Node) (string, bool) {
		if v, ok := yamlx.MapString(n, field); ok && v != "" {
			return field + "=" + v, true
		}
		return "", false
	}
}

var (
	serverIdentity = fieldIdentity("url")
	tagIdentity    = fieldIdentity("name")
)

// canonicalIdentity identifies an element by its whole content, for elements
// that have no name of their own such as security requirements.
func canonicalIdentity(n *yaml.Node) (string, bool) {
	return "canonical=" + yamlx.Canonical(n), true
}

// parameterIdentity follows the spec: a parameter is identified by the pair
// (name, in). A $ref parameter is identified by what it points at.
func parameterIdentity(n *yaml.Node) (string, bool) {
	if ref, ok := yamlx.MapString(n, "$ref"); ok && ref != "" {
		return "$ref=" + ref, true
	}
	name, hasName := yamlx.MapString(n, "name")
	in, hasIn := yamlx.MapString(n, "in")
	if hasName && hasIn {
		return "param=" + in + "\x00" + name, true
	}
	return "", false
}

func mergeRootServers(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error {
	return c.mergeRootSequence(SectionServers, key, root, entry, c.m.servers, serverIdentity, CodeInvalidServer, "server without a url")
}

func mergeRootTags(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error {
	return c.mergeRootSequence(SectionTags, key, root, entry, c.m.tags, tagIdentity, CodeInvalidTag, "tag without a name")
}

func mergeRootSecurity(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error {
	return c.mergeRootSequence(SectionSecurity, key, root, entry, c.m.security, canonicalIdentity, CodeInvalidDocumentCode, "security requirement")
}

func (c *mergeCtx) mergeRootSequence(
	section Section, key string, root *yaml.Node, entry yamlx.MapEntry,
	idx seqIndex, id identityFunc, code, what string,
) error {
	if !yamlx.IsSequence(entry.Value) {
		return c.skipUnexpectedKind(key, "a sequence", entry)
	}
	dst := c.ensureSequence(root, key, entry.KeyN)
	if dst == nil {
		return c.skipUnexpectedKind(key, "a sequence", entry)
	}
	return c.mergeSequence(section, "/"+yamlx.EscapeToken(key), dst, entry.Value, idx, id, code, what)
}

// mergeSequence appends the elements of src that dst does not already carry.
//
// An element whose identifying field is missing is reported rather than fatal:
// the previous implementation asserted the field's type and panicked on any
// spec that omitted it.
func (c *mergeCtx) mergeSequence(
	section Section, ptr string, dst, src *yaml.Node,
	idx seqIndex, id identityFunc, code, what string,
) error {
	for _, item := range src.Content {
		key, natural := id(item)
		if !natural {
			c.m.warn(Diagnostic{
				Code:    code,
				Message: fmt.Sprintf("%s in %s cannot be identified; keeping it as-is", what, ptr),
				At:      c.loc(item),
				Pointer: ptr,
			})
			key, _ = canonicalIdentity(item)
		}

		prev, seen := idx[key]
		if !seen || prev.index >= len(dst.Content) {
			dst.Content = append(dst.Content, yamlx.Clone(item))
			idx[key] = seqEntry{index: len(dst.Content) - 1, at: c.loc(item)}
			continue
		}

		existing := dst.Content[prev.index]
		if yamlx.Equal(existing, item) {
			continue
		}

		elemPtr := ptr + "/" + strconv.Itoa(prev.index)
		incoming := c.loc(item)
		switch c.m.opts.policyFor(section) {
		case ConflictFirstWins:
			c.m.recordConflict(
				Conflict{Section: section, Pointer: elemPtr, Kept: prev.at, Dropped: incoming, Resolution: "first-wins"},
				Diagnostic{
					Code:    CodeConflict,
					Message: fmt.Sprintf("%s at %s is defined differently in two inputs; keeping the first", what, elemPtr),
					At:      prev.at,
					Pointer: elemPtr,
					Related: []Location{incoming},
				})
		case ConflictLastWins:
			dst.Content[prev.index] = yamlx.Clone(item)
			idx[key] = seqEntry{index: prev.index, at: incoming}
			c.m.recordConflict(
				Conflict{Section: section, Pointer: elemPtr, Kept: incoming, Dropped: prev.at, Resolution: "last-wins"},
				Diagnostic{
					Code:    CodeConflict,
					Message: fmt.Sprintf("%s at %s is defined differently in two inputs; keeping the last", what, elemPtr),
					At:      incoming,
					Pointer: elemPtr,
					Related: []Location{prev.at},
				})
		default:
			return newError("merge", ErrConflict, Diagnostic{
				Code:    CodeConflict,
				Message: fmt.Sprintf("%s at %s is defined differently in two inputs", what, elemPtr),
				At:      incoming,
				Pointer: elemPtr,
				Related: []Location{prev.at},
			})
		}
	}
	return nil
}

// buildSeqIndex indexes an existing sequence, for the nested lists whose dedup
// scope is one container rather than the whole document.
func (c *mergeCtx) buildSeqIndex(dst *yaml.Node, id identityFunc) seqIndex {
	idx := seqIndex{}
	if dst == nil {
		return idx
	}
	for i, item := range dst.Content {
		key, natural := id(item)
		if !natural {
			key, _ = canonicalIdentity(item)
		}
		if _, exists := idx[key]; !exists {
			idx[key] = seqEntry{index: i, at: c.loc(item)}
		}
	}
	return idx
}

// mergeScalarSet unions a list of scalars, such as Swagger 2.0's schemes.
// Order of first appearance is preserved.
func mergeScalarSet(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error {
	if !yamlx.IsSequence(entry.Value) {
		return c.skipUnexpectedKind(key, "a sequence", entry)
	}
	dst := c.ensureSequence(root, key, entry.KeyN)
	if dst == nil {
		return c.skipUnexpectedKind(key, "a sequence", entry)
	}
	seen := make(map[string]bool, len(dst.Content))
	for _, item := range dst.Content {
		seen[yamlx.Canonical(item)] = true
	}
	for _, item := range entry.Value.Content {
		k := yamlx.Canonical(item)
		if seen[k] {
			continue
		}
		seen[k] = true
		dst.Content = append(dst.Content, yamlx.Clone(item))
	}
	return nil
}
