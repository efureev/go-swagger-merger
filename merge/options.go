package merge

import "fmt"

// Section names a top-level area of the merge, used to scope conflict policy.
type Section string

// Sections that can carry their own conflict policy.
const (
	SectionInfo         Section = "info"
	SectionServers      Section = "servers"
	SectionTags         Section = "tags"
	SectionPaths        Section = "paths"
	SectionWebhooks     Section = "webhooks"
	SectionComponents   Section = "components"
	SectionSchemas      Section = "schemas"
	SectionResponses    Section = "responses"
	SectionParameters   Section = "parameters"
	SectionSecurity     Section = "security"
	SectionExternalDocs Section = "externalDocs"
	SectionExtensions   Section = "extensions"
	SectionRoot         Section = "root"
)

// ConflictPolicy decides what happens when two inputs define the same key
// with different content. Structurally identical definitions never reach a
// policy: they are deduplicated silently.
type ConflictPolicy uint8

// Conflict policies. The zero value is the strict one on purpose, so that a
// zero Options behaves safely.
const (
	ConflictError ConflictPolicy = iota
	ConflictFirstWins
	ConflictLastWins
)

func (p ConflictPolicy) String() string {
	switch p {
	case ConflictFirstWins:
		return "first-wins"
	case ConflictLastWins:
		return "last-wins"
	default:
		return "error"
	}
}

// ParseConflictPolicy accepts the CLI spellings of a policy.
func ParseConflictPolicy(s string) (ConflictPolicy, error) {
	switch s {
	case "error", "":
		return ConflictError, nil
	case "first", "first-wins":
		return ConflictFirstWins, nil
	case "last", "last-wins":
		return ConflictLastWins, nil
	default:
		return ConflictError, fmt.Errorf("unknown conflict policy %q (want error, first or last)", s)
	}
}

// Reporter receives diagnostics as they are produced. Diagnostics are also
// accumulated on the Result, so a Reporter is only needed for streaming output.
type Reporter interface {
	Report(Diagnostic)
}

// ReporterFunc adapts a function to Reporter.
type ReporterFunc func(Diagnostic)

// Report implements Reporter.
func (f ReporterFunc) Report(d Diagnostic) { f(d) }

// Options configures a merge.
//
// Field names are chosen so that the zero value is the recommended strict
// configuration: conflicts are errors, $refs are validated, input key order is
// preserved. That way merge.Merge(ctx, merge.Options{}, srcs...) does the right
// thing without ceremony.
type Options struct {
	// OnConflict is the default policy for every section.
	OnConflict ConflictPolicy

	// SectionPolicy overrides OnConflict for individual sections.
	SectionPolicy map[Section]ConflictPolicy

	// Base names the Source whose info block and spec version win. Empty
	// means the first source added.
	Base string

	// SortKeys emits a canonical key order instead of input order.
	SortKeys bool

	// SkipRefValidation disables the post-merge local $ref check.
	SkipRefValidation bool

	// AllowVersionSkew permits mixing 3.0.x with 3.1.x inputs.
	AllowVersionSkew bool

	// Strict promotes warnings to errors.
	Strict bool

	// AllowEmptyDocuments accepts empty inputs instead of rejecting them.
	AllowEmptyDocuments bool

	// ReportUnusedComponents notes definitions that no $ref points at.
	ReportUnusedComponents bool

	// AliasBudget caps the nodes produced by expanding YAML anchors. Zero
	// selects a sane default.
	AliasBudget int

	// Reporter receives diagnostics as they happen.
	Reporter Reporter
}

// policyFor resolves the policy for a section, falling back to OnConflict.
func (o *Options) policyFor(s Section) ConflictPolicy {
	if p, ok := o.SectionPolicy[s]; ok {
		return p
	}
	return o.OnConflict
}
