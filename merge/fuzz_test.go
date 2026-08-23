package merge_test

import (
	"context"
	"testing"

	"github.com/efureev/go-swagger-merger/v2/merge"
)

// FuzzMerge asserts the one invariant that matters for a tool run over
// third-party specs in CI: arbitrary input produces a document or an error,
// never a panic. The implementation this replaces asserted types without
// checking them in eight places and crashed on empty files, null sections and
// servers with no url.
func FuzzMerge(f *testing.F) {
	seeds := []string{
		specA,
		specB,
		"",
		"openapi: 3.0.3\n",
		"swagger: \"2.0\"\npaths: {}\n",
		"- a\n- b\n",
		"a: &x\n  b: *x\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\nservers: [{}]\n",
		"openapi: 3.0.3\npaths: {/a: {get: null}}\n",
		"components: {schemas: {A: {$ref: '#/components/schemas/B'}}}\n",
	}
	for _, a := range seeds {
		for _, b := range seeds {
			f.Add(a, b)
		}
	}

	policies := []merge.ConflictPolicy{
		merge.ConflictError, merge.ConflictFirstWins, merge.ConflictLastWins,
	}

	f.Fuzz(func(t *testing.T, a, b string) {
		for _, policy := range policies {
			for _, sortKeys := range []bool{false, true} {
				opts := merge.Options{
					OnConflict:             policy,
					SortKeys:               sortKeys,
					AllowEmptyDocuments:    true,
					AllowVersionSkew:       true,
					ReportUnusedComponents: true,
					// Keep anchor expansion cheap so the fuzzer spends its
					// time on merge logic rather than on allocation.
					AliasBudget: 1 << 14,
				}
				res, err := merge.Merge(context.Background(), opts,
					merge.BytesSource("a.yaml", []byte(a)),
					merge.BytesSource("b.yaml", []byte(b)),
				)
				if err != nil {
					continue
				}
				// A successful merge must always re-encode.
				if _, err := res.Document.YAML(2); err != nil {
					t.Fatalf("merged document does not encode as YAML: %v", err)
				}
				if _, err := res.Document.JSON(0); err != nil {
					t.Fatalf("merged document does not encode as JSON: %v", err)
				}
			}
		}
	})
}
