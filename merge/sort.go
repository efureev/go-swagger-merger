package merge

import (
	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// Canonical root key order. Anything unlisted sorts after, alphabetically, so
// the ordering is total and therefore reproducible.
var (
	openapi3RootOrder = []string{
		"openapi", "info", "jsonSchemaDialect", "servers", "security",
		"tags", "externalDocs", "paths", "webhooks", "components",
	}
	swagger2RootOrder = []string{
		"swagger", "info", "host", "basePath", "schemes", "consumes", "produces",
		"security", "tags", "externalDocs", "paths",
		"definitions", "parameters", "responses", "securityDefinitions",
	}
)

// sortDocument applies canonical ordering. Output is deterministic either way
// -- input order is preserved exactly when this is off -- so sorting is about
// producing a stable shape across inputs that differ only in layout.
func sortDocument(root *yaml.Node, f Family) {
	order := swagger2RootOrder
	containers := []string{"definitions", "parameters", "responses", "securityDefinitions"}
	if f != FamilySwagger2 {
		order = openapi3RootOrder
		containers = nil
	}

	rank := make(map[string]int, len(order))
	for i, k := range order {
		rank[k] = i
	}
	yamlx.SortMapping(root, func(a, b string) bool {
		ra, oka := rank[a]
		rb, okb := rank[b]
		switch {
		case oka && okb:
			return ra < rb
		case oka:
			return true
		case okb:
			return false
		default:
			return a < b
		}
	})

	byName := func(a, b string) bool { return a < b }

	for _, key := range []string{"paths", "webhooks"} {
		if node, ok := yamlx.MapValue(root, key); ok {
			yamlx.SortMapping(node, byName)
		}
	}

	if components, ok := yamlx.MapValue(root, "components"); ok && yamlx.IsMapping(components) {
		yamlx.SortMapping(components, byName)
		for _, e := range yamlx.Entries(components) {
			yamlx.SortMapping(e.Value, byName)
		}
	}
	for _, key := range containers {
		if node, ok := yamlx.MapValue(root, key); ok {
			yamlx.SortMapping(node, byName)
		}
	}
}
