package merge

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// mergePathsSection merges paths (and, in 3.1, webhooks) one operation at a
// time.
//
// This is the defect the rewrite exists for. Replacing a whole path item, as
// the previous implementation did, silently discarded every operation the
// incoming file did not redefine: merging {/users: get} with {/users: post}
// produced a document with only post.
func mergePathsSection(section Section) sectionFunc {
	return func(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error {
		if !yamlx.IsMapping(entry.Value) {
			return c.skipUnexpectedKind(key, "a mapping", entry)
		}
		dst := c.ensureMapping(root, key, entry.KeyN)
		if dst == nil {
			return c.skipUnexpectedKind(key, "a mapping", entry)
		}

		base := "/" + yamlx.EscapeToken(key)
		for _, pe := range yamlx.Entries(entry.Value) {
			ptr := base + "/" + yamlx.EscapeToken(pe.Key)
			if section == SectionPaths {
				c.checkPathTemplate(pe, ptr)
			}

			existing, ok := yamlx.MapValue(dst, pe.Key)
			if !ok {
				c.install(dst, ptr, pe)
				c.indexPathItem(ptr, pe.Value)
				continue
			}
			if yamlx.Equal(existing, pe.Value) {
				continue
			}
			if !yamlx.IsMapping(existing) || !yamlx.IsMapping(pe.Value) {
				if err := c.resolveConflict(section, ptr, dst, pe); err != nil {
					return err
				}
				continue
			}
			if err := c.mergePathItem(section, ptr, existing, pe.Value); err != nil {
				return err
			}
		}
		return nil
	}
}

// mergePathItem combines two definitions of the same path.
func (c *mergeCtx) mergePathItem(section Section, ptr string, dst, src *yaml.Node) error {
	for _, e := range yamlx.Entries(src) {
		switch {
		case e.Key == "parameters" && yamlx.IsSequence(e.Value):
			if err := c.mergeNestedSequence(section, ptr, dst, e, parameterIdentity,
				CodeInvalidDocumentCode, "parameter"); err != nil {
				return err
			}

		case e.Key == "servers" && yamlx.IsSequence(e.Value):
			if err := c.mergeNestedSequence(section, ptr, dst, e, serverIdentity,
				CodeInvalidServer, "server without a url"); err != nil {
				return err
			}

		default:
			// Operations, $ref, summary, description and extensions all reduce
			// to the same rule: identical is a no-op, different is a conflict.
			if err := c.put(section, ptr, dst, e); err != nil {
				return err
			}
		}
	}
	return nil
}

// mergeNestedSequence merges a list whose deduplication scope is this
// container only, rather than the whole document.
func (c *mergeCtx) mergeNestedSequence(
	section Section, parentPtr string, dst *yaml.Node, entry yamlx.MapEntry,
	id identityFunc, code, what string,
) error {
	ptr := parentPtr + "/" + yamlx.EscapeToken(entry.Key)

	target := c.ensureSequence(dst, entry.Key, entry.KeyN)
	if target == nil {
		return c.skipUnexpectedKind(entry.Key, "a sequence", entry)
	}
	idx := c.buildSeqIndex(target, id)
	return c.mergeSequence(section, ptr, target, entry.Value, idx, id, code, what)
}

// indexPathItem records where each operation of a freshly installed path item
// came from, so a later conflict on that operation can name both sides.
func (c *mergeCtx) indexPathItem(ptr string, item *yaml.Node) {
	if !yamlx.IsMapping(item) {
		return
	}
	for _, e := range yamlx.Entries(item) {
		c.m.origin[ptr+"/"+yamlx.EscapeToken(e.Key)] = c.loc(e.Value)
	}
}

// checkPathTemplate reports paths that differ only in the spelling of their
// template parameters. OpenAPI treats /users/{id} and /users/{userId} as the
// same endpoint, so a document holding both is invalid however it was built.
func (c *mergeCtx) checkPathTemplate(entry yamlx.MapEntry, ptr string) {
	norm := normalizePathTemplate(entry.Key)
	if norm == entry.Key {
		return
	}
	first, seen := c.m.pathTemplates[norm]
	if !seen {
		c.m.pathTemplates[norm] = entry.Key
		return
	}
	if first == entry.Key {
		return
	}
	c.m.warn(Diagnostic{
		Code: CodeDuplicatePathTmpl,
		Message: fmt.Sprintf(
			"%q and %q are the same endpoint; parameter names do not distinguish paths", first, entry.Key),
		At:      c.loc(entry.Value),
		Pointer: ptr,
	})
}

// normalizePathTemplate erases template parameter names: /a/{id} -> /a/{}.
func normalizePathTemplate(p string) string {
	if !strings.ContainsRune(p, '{') {
		return p
	}
	var sb strings.Builder
	sb.Grow(len(p))
	depth := 0
	for _, r := range p {
		switch r {
		case '{':
			if depth == 0 {
				sb.WriteString("{}")
			}
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				sb.WriteRune(r)
			}
		}
	}
	return sb.String()
}
