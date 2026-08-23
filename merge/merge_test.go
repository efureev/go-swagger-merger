package merge_test

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/efureev/go-swagger-merger/v2/merge"
)

const specA = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
servers:
  - url: https://a.example.com
tags:
  - name: alpha
paths:
  /a:
    get:
      responses:
        "200": {description: ok}
components:
  schemas:
    A: {type: object}
`

const specB = `
openapi: 3.0.3
info: {title: B, version: "1.0"}
servers:
  - url: https://b.example.com
tags:
  - name: beta
paths:
  /b:
    get:
      responses:
        "200": {description: ok}
components:
  schemas:
    B: {type: object}
`

func mergeStrings(t *testing.T, opts merge.Options, docs ...string) *merge.Result {
	t.Helper()
	res, err := mergeStringsErr(opts, docs...)
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}
	return res
}

func mergeStringsErr(opts merge.Options, docs ...string) (*merge.Result, error) {
	srcs := make([]merge.Source, len(docs))
	for i, d := range docs {
		srcs[i] = merge.BytesSource(letterName(i), []byte(d))
	}
	return merge.Merge(context.Background(), opts, srcs...)
}

func letterName(i int) string {
	return string(rune('a'+i%26)) + ".yaml"
}

func yamlOf(t *testing.T, res *merge.Result) string {
	t.Helper()
	out, err := res.Document.YAML(2)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

// TestTwoMergersIndependent pins the defect that motivated the rewrite: the
// previous implementation kept its server and tag dedup sets in package-level
// maps, so the second Merger created in a process silently dropped every
// server and tag the first had already seen.
func TestTwoMergersIndependent(t *testing.T) {
	for i := 0; i < 3; i++ {
		res := mergeStrings(t, merge.Options{}, specA, specB)
		got := yamlOf(t, res)

		for _, want := range []string{
			"https://a.example.com", "https://b.example.com",
			"name: alpha", "name: beta",
		} {
			if !strings.Contains(got, want) {
				t.Fatalf("merger #%d lost %q:\n%s", i+1, want, got)
			}
		}
	}
}

// TestConcurrentMergesAreIndependent is the -race counterpart: nothing in the
// package may be shared between concurrent merges.
func TestConcurrentMergesAreIndependent(t *testing.T) {
	const workers = 16
	var wg sync.WaitGroup
	results := make([]string, workers)

	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			res, err := mergeStringsErr(merge.Options{}, specA, specB)
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			out, err := res.Document.YAML(2)
			if err != nil {
				t.Errorf("worker %d: %v", i, err)
				return
			}
			results[i] = string(out)
		}(i)
	}
	wg.Wait()

	for i, got := range results {
		if got != results[0] {
			t.Fatalf("worker %d produced different output:\n%s\n--- vs ---\n%s", i, got, results[0])
		}
	}
}

// TestDeterministicOutput pins the other headline defect. The previous
// implementation round-tripped through map[string]any, so key order came from
// Go's randomised map iteration and every run produced a different file.
func TestDeterministicOutput(t *testing.T) {
	const runs = 50
	first := yamlOf(t, mergeStrings(t, merge.Options{}, specA, specB))
	for i := 1; i < runs; i++ {
		if got := yamlOf(t, mergeStrings(t, merge.Options{}, specA, specB)); got != first {
			t.Fatalf("run %d differs from run 0:\n--- run %d ---\n%s\n--- run 0 ---\n%s", i, i, got, first)
		}
	}
}

func TestDeterministicOutputSorted(t *testing.T) {
	opts := merge.Options{SortKeys: true}
	first := yamlOf(t, mergeStrings(t, opts, specA, specB))
	for i := 1; i < 20; i++ {
		if got := yamlOf(t, mergeStrings(t, opts, specA, specB)); got != first {
			t.Fatalf("sorted run %d differs from run 0", i)
		}
	}
}

// TestGarbageInputNeverPanics covers the shapes that made the previous
// implementation assert a type and crash.
func TestGarbageInputNeverPanics(t *testing.T) {
	garbage := []string{
		"",
		"just a scalar",
		"- a\n- list\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\nservers: null\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\nservers: not-a-list\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\nservers: [{}]\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\nservers: [1, 2]\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\ntags: [{}]\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\ntags: {a: b}\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\npaths: 42\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\npaths: {/a: 5}\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\ncomponents: []\n",
		"openapi: 3.0.3\ninfo: {title: X, version: \"1\"}\ncomponents: {schemas: 7}\n",
		"openapi: []\n",
		"openapi: 3.0.3\ninfo: null\npaths: null\n",
		"swagger: banana\n",
		"openapi: 3.0.3\n\x00\n",
		"{{{{",
		"a: &x [*x]\n",
	}

	for _, doc := range garbage {
		for _, other := range []string{specA, doc} {
			// Both orders: garbage first and garbage second.
			_, _ = mergeStringsErr(merge.Options{AllowEmptyDocuments: true}, doc, other)
			_, _ = mergeStringsErr(merge.Options{AllowEmptyDocuments: true}, other, doc)
		}
	}
}

func TestZeroOptionsIsStrict(t *testing.T) {
	const conflicting = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths: {}
components:
  schemas:
    A: {type: string}
`
	_, err := mergeStringsErr(merge.Options{}, specA, conflicting)
	if !errors.Is(err, merge.ErrConflict) {
		t.Fatalf("zero Options should reject a conflict, got %v", err)
	}
}

func TestConflictPolicies(t *testing.T) {
	const first = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths: {}
components: {schemas: {S: {type: integer}}}
`
	const second = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths: {}
components: {schemas: {S: {type: string}}}
`
	tests := []struct {
		policy merge.ConflictPolicy
		want   string
	}{
		{merge.ConflictFirstWins, "integer"},
		{merge.ConflictLastWins, "string"},
	}
	for _, tc := range tests {
		t.Run(tc.policy.String(), func(t *testing.T) {
			res := mergeStrings(t, merge.Options{OnConflict: tc.policy}, first, second)
			if got := yamlOf(t, res); !strings.Contains(got, tc.want) {
				t.Errorf("want %q in output:\n%s", tc.want, got)
			}
			if len(res.Conflicts) != 1 {
				t.Fatalf("want 1 conflict, got %d", len(res.Conflicts))
			}
			c := res.Conflicts[0]
			if c.Pointer != "/components/schemas/S" {
				t.Errorf("pointer = %q", c.Pointer)
			}
			if c.Kept.Source == "" || c.Dropped.Source == "" {
				t.Errorf("conflict should name both sides, got %+v", c)
			}
		})
	}
}

// TestStrictDoesNotOverrideConflictPolicy: an explicit first/last-wins is a
// decision, so --strict must not turn it back into a failure.
func TestStrictDoesNotOverrideConflictPolicy(t *testing.T) {
	const a = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths: {}
components: {schemas: {S: {type: integer}}}
`
	const b = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths: {}
components: {schemas: {S: {type: string}}}
`
	if _, err := mergeStringsErr(merge.Options{OnConflict: merge.ConflictFirstWins, Strict: true}, a, b); err != nil {
		t.Fatalf("strict promoted an explicitly resolved conflict: %v", err)
	}
}

func TestStrictPromotesOtherWarnings(t *testing.T) {
	const withExternalRef = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths:
  /x:
    get:
      responses:
        "200": {$ref: "./other.yaml#/components/responses/OK"}
`
	_, err := mergeStringsErr(merge.Options{Strict: true}, withExternalRef)
	if !errors.Is(err, merge.ErrStrict) {
		t.Fatalf("want ErrStrict, got %v", err)
	}
}

// TestInputsAreNotMutated: the merged tree must be a copy, or a caller that
// merges the same bytes twice would see the first result change.
func TestInputsAreNotMutated(t *testing.T) {
	src := []byte(specA)
	before := string(src)

	res1 := mergeStrings(t, merge.Options{}, specA, specB)
	out1 := yamlOf(t, res1)

	res2 := mergeStrings(t, merge.Options{}, specA, specB)
	_ = yamlOf(t, res2)

	if got := yamlOf(t, res1); got != out1 {
		t.Error("the first result changed after a second merge")
	}
	if string(src) != before {
		t.Error("input bytes were modified")
	}
}

func TestMergerIsPoisonedAfterError(t *testing.T) {
	m := merge.New(merge.Options{})
	if err := m.AddBytes("a.yaml", []byte(specA)); err != nil {
		t.Fatal(err)
	}
	if err := m.AddBytes("bad.yaml", []byte("a scalar")); err == nil {
		t.Fatal("expected an error for a scalar document")
	}
	if err := m.AddBytes("b.yaml", []byte(specB)); err == nil {
		t.Fatal("a poisoned Merger should keep returning its first error")
	}
	if _, err := m.Result(); err == nil {
		t.Fatal("Result should report the sticky error")
	}
}

func TestResultRequiresInput(t *testing.T) {
	if _, err := merge.New(merge.Options{}).Result(); !errors.Is(err, merge.ErrNoInput) {
		t.Fatalf("want ErrNoInput, got %v", err)
	}
}

func TestFSSource(t *testing.T) {
	fsys := fstest.MapFS{
		"specs/a.yaml": {Data: []byte(specA)},
		"specs/b.yaml": {Data: []byte(specB)},
	}
	res, err := merge.Merge(context.Background(), merge.Options{},
		merge.FSSource(fsys, "specs/a.yaml"),
		merge.FSSource(fsys, "specs/b.yaml"),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := yamlOf(t, res); !strings.Contains(got, "/a:") || !strings.Contains(got, "/b:") {
		t.Errorf("both documents should be present:\n%s", got)
	}
}

func TestReaderSource(t *testing.T) {
	res, err := merge.Merge(context.Background(), merge.Options{},
		merge.ReaderSource("<stdin>", strings.NewReader(specA)),
		merge.BytesSource("b.yaml", []byte(specB)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if got := yamlOf(t, res); !strings.Contains(got, "/a:") {
		t.Errorf("stdin document missing:\n%s", got)
	}
}

func TestDocumentDecode(t *testing.T) {
	res := mergeStrings(t, merge.Options{}, specA, specB)

	var spec struct {
		OpenAPI string `yaml:"openapi"`
		Info    struct {
			Title string `yaml:"title"`
		} `yaml:"info"`
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := res.Document.Decode(&spec); err != nil {
		t.Fatal(err)
	}
	if spec.OpenAPI != "3.0.3" || spec.Info.Title != "A" {
		t.Errorf("decoded %+v", spec)
	}
	if len(spec.Paths) != 2 {
		t.Errorf("want 2 paths, got %d", len(spec.Paths))
	}
}

func TestContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := merge.Merge(ctx, merge.Options{},
		merge.BytesSource("a.yaml", []byte(specA)))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("want context.Canceled, got %v", err)
	}
}

func TestParseConflictPolicy(t *testing.T) {
	tests := map[string]struct {
		want    merge.ConflictPolicy
		wantErr bool
	}{
		"":           {merge.ConflictError, false},
		"error":      {merge.ConflictError, false},
		"first":      {merge.ConflictFirstWins, false},
		"first-wins": {merge.ConflictFirstWins, false},
		"last":       {merge.ConflictLastWins, false},
		"last-wins":  {merge.ConflictLastWins, false},
		"nonsense":   {merge.ConflictError, true},
	}
	for in, tc := range tests {
		got, err := merge.ParseConflictPolicy(in)
		if (err != nil) != tc.wantErr {
			t.Errorf("ParseConflictPolicy(%q) err = %v, wantErr %v", in, err, tc.wantErr)
		}
		if got != tc.want {
			t.Errorf("ParseConflictPolicy(%q) = %v, want %v", in, got, tc.want)
		}
	}
}

func TestVersionCompatibility(t *testing.T) {
	v30 := merge.SpecVersion{Family: merge.FamilyOpenAPI3, Major: 3, Minor: 0, Patch: 3}
	v31 := merge.SpecVersion{Family: merge.FamilyOpenAPI3, Major: 3, Minor: 1}
	v20 := merge.SpecVersion{Family: merge.FamilySwagger2, Major: 2}

	if v30.CompatibleWith(v20, true) {
		t.Error("swagger 2 and openapi 3 must never merge")
	}
	if v30.CompatibleWith(v31, false) {
		t.Error("3.0 and 3.1 should need --allow-version-skew")
	}
	if !v30.CompatibleWith(v31, true) {
		t.Error("--allow-version-skew should permit 3.0 with 3.1")
	}
	if !v30.Precedes(v31) {
		t.Error("3.0.3 precedes 3.1")
	}
}

func TestResultIsIdempotent(t *testing.T) {
	m := merge.New(merge.Options{})
	if err := m.AddBytes("a.yaml", []byte(specA)); err != nil {
		t.Fatal(err)
	}
	first, err := m.Result()
	if err != nil {
		t.Fatal(err)
	}
	second, err := m.Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Diagnostics) != len(second.Diagnostics) {
		t.Errorf("calling Result twice changed the diagnostics: %d then %d",
			len(first.Diagnostics), len(second.Diagnostics))
	}
	a, _ := first.Document.YAML(2)
	b, _ := second.Document.YAML(2)
	if string(a) != string(b) {
		t.Errorf("calling Result twice changed the document:\n%s\n---\n%s", a, b)
	}
}

// Merging a document with itself must reproduce it: everything deduplicates.
func TestMergeIsIdempotentOverOneDocument(t *testing.T) {
	once := yamlOf(t, mergeStrings(t, merge.Options{}, specA))
	twice := yamlOf(t, mergeStrings(t, merge.Options{}, specA, specA))
	if once != twice {
		t.Errorf("merging a document with itself changed it:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
	}
}

func TestPercentEncodedRefResolves(t *testing.T) {
	const doc = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths:
  /users/{id}:
    get:
      responses:
        "200": {description: ok}
components:
  pathItems:
    Alias: {$ref: "#/paths/~1users~1%7Bid%7D"}
`
	if _, err := mergeStringsErr(merge.Options{}, doc); err != nil {
		t.Fatalf("a percent-encoded fragment should resolve: %v", err)
	}
}

// ── Regressions from the post-rewrite review ────────────────────────────────

// A $ref inside an example is payload data, not a reference. Validating it
// failed perfectly valid documents.
func TestRefInsideDataFieldsIsNotAReference(t *testing.T) {
	for _, field := range []string{"example", "default", "const"} {
		t.Run(field, func(t *testing.T) {
			doc := `
openapi: 3.1.0
info: {title: Lit, version: "1.0"}
paths: {}
components:
  schemas:
    Doc:
      type: object
      ` + field + `:
        $ref: "#/definitions/LegacyThing"
        note: literal payload
`
			if _, err := mergeStringsErr(merge.Options{}, doc); err != nil {
				t.Fatalf("a $ref inside %s must not be validated: %v", field, err)
			}
		})
	}
}

func TestRefInsideEnumAndExampleValueIsNotAReference(t *testing.T) {
	const doc = `
openapi: 3.0.3
info: {title: Lit, version: "1.0"}
paths: {}
components:
  schemas:
    Doc:
      enum:
        - {$ref: "#/nope/one"}
  examples:
    Sample:
      value:
        $ref: "#/nope/two"
`
	if _, err := mergeStringsErr(merge.Options{}, doc); err != nil {
		t.Fatalf("enum values and example payloads must not be validated: %v", err)
	}
}

// A schema named "example" is a definition, not an example, so its contents
// must still be validated.
func TestComponentNamedExampleIsStillValidated(t *testing.T) {
	const doc = `
openapi: 3.0.3
info: {title: Lit, version: "1.0"}
paths: {}
components:
  schemas:
    example:
      properties:
        broken: {$ref: "#/components/schemas/Absent"}
`
	if _, err := mergeStringsErr(merge.Options{}, doc); !errors.Is(err, merge.ErrDanglingRef) {
		t.Fatalf("want ErrDanglingRef inside a schema called \"example\", got %v", err)
	}
}

// "properties" keys are author-chosen, so a property called $ref is a property.
func TestPropertyNamedRefIsNotAReference(t *testing.T) {
	const doc = `
openapi: 3.0.3
info: {title: Meta, version: "1.0"}
paths: {}
components:
  schemas:
    JsonSchemaDoc:
      type: object
      properties:
        $ref: {type: string}
        $id: {type: string}
`
	res, err := mergeStringsErr(merge.Options{}, doc)
	if err != nil {
		t.Fatalf("a property named $ref must not be read as a reference: %v", err)
	}
	for _, d := range res.Warnings() {
		if d.Code == merge.CodeRefSiblings {
			t.Errorf("unexpected ref-sibling warning: %s", d)
		}
	}
}

// Every input carries its own info block, so the base-override notice is
// unavoidable and must not make --strict fail on an ordinary merge.
func TestStrictIgnoresBaseOverride(t *testing.T) {
	const one = `
openapi: 3.0.3
info: {title: One, version: "1.0"}
paths: {/a: {get: {responses: {"200": {description: ok}}}}}
`
	const two = `
openapi: 3.0.3
info: {title: Two, version: "2.0"}
paths: {/b: {get: {responses: {"200": {description: ok}}}}}
`
	res, err := mergeStringsErr(merge.Options{Strict: true}, one, two)
	if err != nil {
		t.Fatalf("--strict must survive differing info blocks: %v", err)
	}
	var sawBaseOverride bool
	for _, d := range res.Warnings() {
		if d.Code == merge.CodeBaseOverride {
			sawBaseOverride = true
		}
	}
	if !sawBaseOverride {
		t.Error("the base-override warning should still be reported, just not promoted")
	}
}

// The surviving side of a nested-sequence conflict must be attributed to the
// file it came from, not to whichever document is being merged at the time.
func TestNestedSequenceConflictNamesTheRightFile(t *testing.T) {
	const first = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths:
  /u:
    parameters:
      - {name: id, in: path, required: true, schema: {type: string}}
    get: {responses: {"200": {description: ok}}}
`
	const second = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths:
  /u:
    parameters:
      - {name: id, in: path, required: true, schema: {type: integer}}
    post: {responses: {"201": {description: ok}}}
`
	res := mergeStrings(t, merge.Options{OnConflict: merge.ConflictFirstWins}, first, second)
	if len(res.Conflicts) != 1 {
		t.Fatalf("want 1 conflict, got %d", len(res.Conflicts))
	}
	c := res.Conflicts[0]
	if c.Kept.Source != "a.yaml" {
		t.Errorf("kept side attributed to %q, want a.yaml", c.Kept.Source)
	}
	if c.Dropped.Source != "b.yaml" {
		t.Errorf("dropped side attributed to %q, want b.yaml", c.Dropped.Source)
	}
}

// A conflict found while deep-merging must still name both sides: the
// intermediate pointers were never installed in their own right.
func TestDeepMergeConflictNamesBothSides(t *testing.T) {
	const first = `{openapi: 3.0.3, info: {title: G, version: "1"}, paths: {}, x-custom: {a: {b: 1}}}`
	const second = `{openapi: 3.0.3, info: {title: G, version: "1"}, paths: {}, x-custom: {a: {b: 2}}}`

	_, err := mergeStringsErr(merge.Options{}, first, second)
	if !errors.Is(err, merge.ErrConflict) {
		t.Fatalf("want ErrConflict, got %v", err)
	}
	var mErr *merge.Error
	if !errors.As(err, &mErr) {
		t.Fatal("error should carry a Diagnostic")
	}
	if mErr.Diag.Pointer != "/x-custom/a/b" {
		t.Errorf("pointer = %q", mErr.Diag.Pointer)
	}
	if len(mErr.Diag.Related) == 0 || mErr.Diag.Related[0].Source == "" {
		t.Errorf("the surviving side is unattributed: %+v", mErr.Diag)
	}
}

// An exported type must not have a zero value that panics.
func TestZeroValueMergerWorks(t *testing.T) {
	var m merge.Merger
	if err := m.AddBytes("z.yaml", []byte(specA)); err != nil {
		t.Fatal(err)
	}
	res, err := m.Result()
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 1 {
		t.Errorf("sources = %v", res.Sources)
	}
}

// Result recomputes its own diagnostics, so finalising twice around an Add
// must not report the same problem again.
func TestRepeatedResultDoesNotDuplicateDiagnostics(t *testing.T) {
	const withExternalRef = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths:
  /x:
    get:
      responses:
        "200": {$ref: "./other.yaml#/components/responses/OK"}
`
	var reported []merge.Diagnostic
	m := merge.New(merge.Options{
		Reporter: merge.ReporterFunc(func(d merge.Diagnostic) { reported = append(reported, d) }),
	})
	if err := m.AddBytes("a.yaml", []byte(withExternalRef)); err != nil {
		t.Fatal(err)
	}
	first, err := m.Result()
	if err != nil {
		t.Fatal(err)
	}
	if err := m.AddBytes("b.yaml", []byte(specB)); err != nil {
		t.Fatal(err)
	}
	second, err := m.Result()
	if err != nil {
		t.Fatal(err)
	}

	count := func(diags []merge.Diagnostic) int {
		n := 0
		for _, d := range diags {
			if d.Code == merge.CodeExternalRef {
				n++
			}
		}
		return n
	}
	if got := count(first.Diagnostics); got != 1 {
		t.Errorf("first Result reported %d external-ref warnings, want 1", got)
	}
	if got := count(second.Diagnostics); got != 1 {
		t.Errorf("second Result reported %d external-ref warnings, want 1", got)
	}
	_ = reported
}

// A document that contributed nothing is not a source, or the leading empty
// file would consume the base slot and leave the merge without one.
func TestSkippedEmptyInputIsNotASource(t *testing.T) {
	const base = `
openapi: 3.0.3
info: {title: Chosen, version: "1.0"}
paths: {/a: {get: {responses: {"200": {description: ok}}}}}
`
	const other = `
openapi: 3.0.3
info: {title: Other, version: "9.9"}
paths: {/b: {get: {responses: {"200": {description: ok}}}}}
`
	res, err := merge.Merge(context.Background(), merge.Options{AllowEmptyDocuments: true},
		merge.BytesSource("empty.yaml", nil),
		merge.BytesSource("base.yaml", []byte(base)),
		merge.BytesSource("other.yaml", []byte(other)),
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Sources) != 2 {
		t.Errorf("sources = %v, want the two documents that contributed", res.Sources)
	}
	if got := yamlOf(t, res); !strings.Contains(got, "title: Chosen") {
		t.Errorf("the first non-empty input should be the base:\n%s", got)
	}
}

// The two options are independent; unused-component reporting must not be
// switched off as a side effect of skipping reference validation.
func TestUnusedComponentsReportedWithoutRefValidation(t *testing.T) {
	const doc = `
openapi: 3.0.3
info: {title: A, version: "1.0"}
paths: {}
components:
  schemas:
    Orphan: {type: object}
`
	res := mergeStrings(t, merge.Options{SkipRefValidation: true, ReportUnusedComponents: true}, doc)
	for _, d := range res.Warnings() {
		if d.Code == merge.CodeUnusedComponent {
			return
		}
	}
	t.Errorf("expected an unused-component warning, got %v", res.Warnings())
}
