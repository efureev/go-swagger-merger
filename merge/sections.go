package merge

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// sectionFunc merges one top-level key of an input document into the
// accumulated root.
type sectionFunc func(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error

// mergeCtx carries the per-document state a section merger needs.
type mergeCtx struct {
	m      *Merger
	source string
	isBase bool
}

func (c *mergeCtx) loc(n *yaml.Node) Location { return nodeLocation(c.source, n) }

// sectionTable picks the dispatch table for a spec family. A table beats the
// switch chain it replaces: adding a section is one entry, and every key that
// is not listed lands on mergeGeneric instead of falling through to whatever
// the last case happened to do.
func sectionTable(f Family) map[string]sectionFunc {
	if f == FamilySwagger2 {
		return swagger2Sections
	}
	return openapi3Sections
}

var openapi3Sections = map[string]sectionFunc{
	"info":              mergeBaseWins(SectionInfo),
	"jsonSchemaDialect": mergeBaseWins(SectionRoot),
	"externalDocs":      mergeBaseWins(SectionExternalDocs),
	"servers":           mergeRootServers,
	"tags":              mergeRootTags,
	"security":          mergeRootSecurity,
	"paths":             mergePathsSection(SectionPaths),
	"webhooks":          mergePathsSection(SectionWebhooks),
	"components":        mergeComponentsSection,
}

var swagger2Sections = map[string]sectionFunc{
	"info":                mergeBaseWins(SectionInfo),
	"host":                mergeBaseWins(SectionRoot),
	"basePath":            mergeBaseWins(SectionRoot),
	"externalDocs":        mergeBaseWins(SectionExternalDocs),
	"schemes":             mergeScalarSet,
	"consumes":            mergeScalarSet,
	"produces":            mergeScalarSet,
	"tags":                mergeRootTags,
	"security":            mergeRootSecurity,
	"paths":               mergePathsSection(SectionPaths),
	"definitions":         mergeTopNamedMap(SectionSchemas),
	"parameters":          mergeTopNamedMap(SectionParameters),
	"responses":           mergeTopNamedMap(SectionResponses),
	"securityDefinitions": mergeTopNamedMap(SectionComponents),
}

// ensureMapping returns the mapping stored at root[key], creating it when
// absent. It returns nil when the key holds something that is not a mapping.
func (c *mergeCtx) ensureMapping(root *yaml.Node, key string, keyNode *yaml.Node) *yaml.Node {
	existing, ok := yamlx.MapValue(root, key)
	if ok {
		if yamlx.IsMapping(existing) {
			return existing
		}
		return nil
	}
	fresh := yamlx.NewMapping()
	kn := yamlx.Clone(keyNode)
	if kn == nil {
		kn = yamlx.NewScalar(key)
	}
	yamlx.MapSetNode(root, kn, fresh)
	return fresh
}

// ensureSequence is ensureMapping for sequences.
func (c *mergeCtx) ensureSequence(root *yaml.Node, key string, keyNode *yaml.Node) *yaml.Node {
	existing, ok := yamlx.MapValue(root, key)
	if ok {
		if yamlx.IsSequence(existing) {
			return existing
		}
		return nil
	}
	fresh := yamlx.NewSequence()
	kn := yamlx.Clone(keyNode)
	if kn == nil {
		kn = yamlx.NewScalar(key)
	}
	yamlx.MapSetNode(root, kn, fresh)
	return fresh
}

// put installs entry into dst, resolving a clash through the section policy.
//
// This is the single place conflicts are decided. Note the middle branch: two
// structurally identical definitions are deduplicated in silence, which is
// what makes an error-by-default policy livable when several inputs legitimately
// carry a copy of the same shared schema.
func (c *mergeCtx) put(section Section, parentPtr string, dst *yaml.Node, entry yamlx.MapEntry) error {
	ptr := parentPtr + "/" + yamlx.EscapeToken(entry.Key)

	existing, ok := yamlx.MapValue(dst, entry.Key)
	if !ok {
		c.install(dst, ptr, entry)
		return nil
	}
	if yamlx.Equal(existing, entry.Value) {
		return nil
	}
	return c.resolveConflict(section, ptr, dst, entry)
}

func (c *mergeCtx) install(dst *yaml.Node, ptr string, entry yamlx.MapEntry) {
	kn := yamlx.Clone(entry.KeyN)
	if kn == nil {
		kn = yamlx.NewScalar(entry.Key)
	}
	yamlx.MapSetNode(dst, kn, yamlx.Clone(entry.Value))
	c.m.origin[ptr] = c.loc(entry.Value)
}

func (c *mergeCtx) resolveConflict(section Section, ptr string, dst *yaml.Node, entry yamlx.MapEntry) error {
	kept := c.m.origin[ptr]
	incoming := c.loc(entry.Value)

	switch c.m.opts.policyFor(section) {
	case ConflictFirstWins:
		c.m.recordConflict(
			Conflict{Section: section, Pointer: ptr, Kept: kept, Dropped: incoming, Resolution: "first-wins"},
			Diagnostic{
				Code:    CodeConflict,
				Message: fmt.Sprintf("%s is defined differently in two inputs; keeping the first", ptr),
				At:      kept,
				Pointer: ptr,
				Related: []Location{incoming},
			})
		return nil

	case ConflictLastWins:
		c.install(dst, ptr, entry)
		c.m.recordConflict(
			Conflict{Section: section, Pointer: ptr, Kept: incoming, Dropped: kept, Resolution: "last-wins"},
			Diagnostic{
				Code:    CodeConflict,
				Message: fmt.Sprintf("%s is defined differently in two inputs; keeping the last", ptr),
				At:      incoming,
				Pointer: ptr,
				Related: []Location{kept},
			})
		return nil

	default:
		return newError("merge", ErrConflict, Diagnostic{
			Code:    CodeConflict,
			Message: fmt.Sprintf("%s is defined differently in two inputs", ptr),
			At:      incoming,
			Pointer: ptr,
			Related: []Location{kept},
		})
	}
}

// mergeBaseWins handles single-valued sections such as info: the base document
// supplies them and other inputs are ignored with a note.
//
// These sections never raise a conflict. Every input file carries its own
// info block by construction, so erroring there would make the tool unusable.
func mergeBaseWins(section Section) sectionFunc {
	return func(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error {
		ptr := "/" + yamlx.EscapeToken(key)

		existing, ok := yamlx.MapValue(root, key)
		if !ok {
			c.install(root, ptr, entry)
			c.m.baseOwned[key] = c.isBase
			return nil
		}
		if yamlx.Equal(existing, entry.Value) {
			return nil
		}
		if c.isBase {
			previous := c.m.origin[ptr]
			c.install(root, ptr, entry)
			c.m.baseOwned[key] = true
			c.warnBaseOnce(ptr, Diagnostic{
				Code:    CodeBaseOverride,
				Message: fmt.Sprintf("%s taken from the base document", ptr),
				At:      c.loc(entry.Value),
				Pointer: ptr,
				Related: []Location{previous},
			})
			return nil
		}
		c.warnBaseOnce(ptr, Diagnostic{
			Code:    CodeBaseOverride,
			Message: fmt.Sprintf("%s comes from %s; the other inputs define it differently", ptr, c.m.origin[ptr].Source),
			At:      c.loc(entry.Value),
			Pointer: ptr,
			Related: []Location{c.m.origin[ptr]},
		})
		return nil
	}
}

// mergeTopNamedMap merges a root-level map of name -> definition, which is how
// Swagger 2.0 spells what OpenAPI 3 keeps under components.
func mergeTopNamedMap(section Section) sectionFunc {
	return func(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error {
		if !yamlx.IsMapping(entry.Value) {
			return c.skipUnexpectedKind(key, "a mapping", entry)
		}
		dst := c.ensureMapping(root, key, entry.KeyN)
		if dst == nil {
			return c.skipUnexpectedKind(key, "a mapping", entry)
		}
		ptr := "/" + yamlx.EscapeToken(key)
		for _, e := range yamlx.Entries(entry.Value) {
			c.checkComponentName(key, ptr, e)
			if err := c.put(section, ptr, dst, e); err != nil {
				return err
			}
		}
		return nil
	}
}

// mergeGeneric handles keys the spec tables do not know: vendor extensions and
// anything a future spec version adds.
//
// It deep-merges mappings key by key and treats every other shape as a single
// value governed by the conflict policy. Guessing how to combine an unknown
// sequence would be worse than reporting it.
func mergeGeneric(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error {
	section := SectionRoot
	if len(key) > 2 && key[0] == 'x' && key[1] == '-' {
		section = SectionExtensions
	}
	return c.mergeValue(section, "", root, entry)
}

const maxGenericDepth = 100

func (c *mergeCtx) mergeValue(section Section, parentPtr string, dst *yaml.Node, entry yamlx.MapEntry) error {
	return c.mergeValueAt(section, parentPtr, dst, entry, 0)
}

func (c *mergeCtx) mergeValueAt(section Section, parentPtr string, dst *yaml.Node, entry yamlx.MapEntry, depth int) error {
	ptr := parentPtr + "/" + yamlx.EscapeToken(entry.Key)

	existing, ok := yamlx.MapValue(dst, entry.Key)
	if !ok {
		c.install(dst, ptr, entry)
		return nil
	}
	if yamlx.Equal(existing, entry.Value) {
		return nil
	}
	if depth < maxGenericDepth && yamlx.IsMapping(existing) && yamlx.IsMapping(entry.Value) {
		for _, sub := range yamlx.Entries(entry.Value) {
			if err := c.mergeValueAt(section, ptr, existing, sub, depth+1); err != nil {
				return err
			}
		}
		return nil
	}
	return c.resolveConflict(section, ptr, dst, entry)
}

// warnBaseOnce reports a base-wins override the first time it happens for a
// key. Repeating it for every remaining input adds no information.
func (c *mergeCtx) warnBaseOnce(ptr string, d Diagnostic) {
	if c.m.baseWarned[ptr] {
		return
	}
	c.m.baseWarned[ptr] = true
	c.m.warn(d)
}

func (c *mergeCtx) skipUnexpectedKind(key, want string, entry yamlx.MapEntry) error {
	c.m.warn(Diagnostic{
		Code:    CodeInvalidDocumentCode,
		Message: fmt.Sprintf("expected %s to be %s, got %s; skipping", key, want, kindName(entry.Value)),
		At:      c.loc(entry.Value),
		Pointer: "/" + yamlx.EscapeToken(key),
	})
	return nil
}
