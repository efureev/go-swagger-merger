package merge

import (
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// componentSections maps a components sub-key to the policy section that
// governs it, so callers can say --on-conflict-section schemas=first.
var componentSections = map[string]Section{
	"schemas":       SectionSchemas,
	"responses":     SectionResponses,
	"parameters":    SectionParameters,
	"examples":      SectionComponents,
	"requestBodies": SectionComponents,
	"headers":       SectionComponents,
	// securitySchemes, links, callbacks and 3.1's pathItems fall through to
	// SectionComponents as well; listing the known names documents them.
	"securitySchemes": SectionComponents,
	"links":           SectionComponents,
	"callbacks":       SectionComponents,
	"pathItems":       SectionComponents,
}

func componentSection(key string) Section {
	if s, ok := componentSections[key]; ok {
		return s
	}
	return SectionComponents
}

// mergeComponentsSection merges each components sub-map by definition name.
func mergeComponentsSection(c *mergeCtx, key string, root *yaml.Node, entry yamlx.MapEntry) error {
	if !yamlx.IsMapping(entry.Value) {
		return c.skipUnexpectedSection(key, "a mapping", entry)
	}
	dst := c.ensureMapping(root, key, entry.KeyN)
	if dst == nil {
		return c.skipUnexpectedSection(key, "a mapping", entry)
	}

	base := "/" + yamlx.EscapeToken(key)
	for _, sub := range yamlx.Entries(entry.Value) {
		if !yamlx.IsMapping(sub.Value) {
			// An extension or a future non-map member of components.
			if err := c.mergeValue(SectionComponents, base, dst, sub); err != nil {
				return err
			}
			continue
		}

		subDst := c.ensureMapping(dst, sub.Key, sub.KeyN)
		if subDst == nil {
			if err := c.mergeValue(SectionComponents, base, dst, sub); err != nil {
				return err
			}
			continue
		}

		section := componentSection(sub.Key)
		subPtr := base + "/" + yamlx.EscapeToken(sub.Key)
		for _, def := range yamlx.Entries(sub.Value) {
			c.checkComponentName(sub.Key, subPtr, def)
			if err := c.put(section, subPtr, subDst, def); err != nil {
				return err
			}
		}
	}
	return nil
}

// checkComponentName reports names that cannot be referenced, since OpenAPI
// restricts component keys to [a-zA-Z0-9._-].
func (c *mergeCtx) checkComponentName(container, parentPtr string, entry yamlx.MapEntry) {
	if validComponentName(entry.Key) {
		return
	}
	c.m.warn(Diagnostic{
		Code:    CodeInvalidComponentName,
		Message: fmt.Sprintf("%q is not a valid %s name; it cannot be referenced by $ref", entry.Key, container),
		At:      c.loc(entry.Value),
		Pointer: parentPtr + "/" + yamlx.EscapeToken(entry.Key),
	})
}

func validComponentName(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '.' || r == '_' || r == '-':
		default:
			return false
		}
	}
	return true
}
