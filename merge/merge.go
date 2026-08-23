package merge

import (
	"context"
	"fmt"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// Result is the outcome of a merge.
type Result struct {
	Document    *Document
	Conflicts   []Conflict
	Diagnostics []Diagnostic
	Sources     []string
}

// Warnings returns the diagnostics that are not errors.
func (r *Result) Warnings() []Diagnostic {
	out := make([]Diagnostic, 0, len(r.Diagnostics))
	for _, d := range r.Diagnostics {
		if d.Severity == SeverityWarning {
			out = append(out, d)
		}
	}
	return out
}

// Merger accumulates documents. All state lives on the value, so independent
// Mergers never interfere and a Merger is safe to use from one goroutine
// while another uses a different one.
type Merger struct {
	opts Options
	root *yaml.Node

	version   SpecVersion
	versionAt Location

	sources   []string
	baseFound bool

	// Dedup indexes for the root-level sequences, carried across Add calls.
	servers  seqIndex
	tags     seqIndex
	security seqIndex

	// pathTemplates maps a path with its parameter names erased to the first
	// spelling seen, which is how equivalent-but-differently-named templates
	// are detected.
	pathTemplates map[string]string

	// origin records where the definition currently held at a pointer came
	// from, so a later conflict can name both sides.
	origin map[string]Location

	// baseOwned marks root keys claimed by the base document.
	baseOwned map[string]bool
	// baseWarned keeps the base-wins notice to one per key.
	baseWarned map[string]bool

	diags     []Diagnostic
	conflicts []Conflict

	// err is sticky: once a merge fails the Merger is poisoned, because the
	// document is left half-merged.
	err error

	// result caches the finalised outcome so that calling Result twice does
	// not re-run validation and duplicate its diagnostics. Add clears it.
	result *Result
}

// seqEntry remembers where a deduplicated sequence element lives and came from.
type seqEntry struct {
	index int
	at    Location
}

type seqIndex map[string]seqEntry

// New returns a Merger configured by opts.
func New(opts Options) *Merger {
	return &Merger{
		opts:          opts,
		root:          yamlx.NewMapping(),
		servers:       seqIndex{},
		tags:          seqIndex{},
		security:      seqIndex{},
		pathTemplates: map[string]string{},
		origin:        map[string]Location{},
		baseOwned:     map[string]bool{},
		baseWarned:    map[string]bool{},
	}
}

// Merge combines srcs into one document. Sources are merged in order; the
// first is the base unless Options.Base names another.
func Merge(ctx context.Context, opts Options, srcs ...Source) (*Result, error) {
	m := New(opts)
	for _, src := range srcs {
		if err := m.Add(ctx, src); err != nil {
			return nil, err
		}
	}
	return m.Result()
}

// AddBytes merges an in-memory document.
func (m *Merger) AddBytes(name string, data []byte) error {
	return m.Add(context.Background(), BytesSource(name, data))
}

// Add merges one source into the accumulated document. The first error
// poisons the Merger: Result will return it and no further Add should be
// attempted.
func (m *Merger) Add(ctx context.Context, src Source) error {
	if m.err != nil {
		return m.err
	}
	m.result = nil
	if err := m.add(ctx, src); err != nil {
		m.err = err
		return err
	}
	return nil
}

func (m *Merger) add(ctx context.Context, src Source) error {
	label := src.Label()

	root, err := loadDocument(ctx, src, &m.opts)
	if err != nil {
		return err
	}
	if root == nil {
		m.warn(Diagnostic{
			Code:    CodeEmptyDocument,
			Message: fmt.Sprintf("%s is empty, skipping", label),
			At:      Location{Source: label},
		})
		m.sources = append(m.sources, label)
		return nil
	}

	isBase := m.isBase(label)
	if isBase {
		m.baseFound = true
	}

	if err := m.adoptVersion(label, root, isBase); err != nil {
		return err
	}

	c := &mergeCtx{m: m, source: label, isBase: isBase}
	table := sectionTable(m.version.Family)

	for _, entry := range yamlx.Entries(root) {
		if err := ctx.Err(); err != nil {
			return err
		}
		// The version key is owned by adoptVersion.
		if entry.Key == "openapi" || entry.Key == "swagger" {
			continue
		}
		fn, ok := table[entry.Key]
		if !ok {
			fn = mergeGeneric
		}
		if err := fn(c, entry.Key, m.root, entry); err != nil {
			return err
		}
	}

	m.sources = append(m.sources, label)
	return nil
}

// isBase reports whether this source supplies info and the spec version.
func (m *Merger) isBase(label string) bool {
	if m.opts.Base == "" {
		return len(m.sources) == 0
	}
	return label == m.opts.Base
}

func (m *Merger) adoptVersion(label string, root *yaml.Node, isBase bool) error {
	v, err := DetectVersion(root)
	if err != nil {
		if isMissingVersionKey(root) {
			m.warn(Diagnostic{
				Code:    CodeInvalidDocumentCode,
				Message: fmt.Sprintf("%s declares no openapi/swagger version", label),
				At:      Location{Source: label},
			})
			return nil
		}
		return newError("version", err, Diagnostic{
			Code:    CodeInvalidDocumentCode,
			Message: fmt.Sprintf("%s has an unusable version", label),
			At:      Location{Source: label},
		})
	}

	at := Location{Source: label}
	if _, vn, ok := yamlx.MapGet(root, v.Family.RootKey()); ok {
		at = nodeLocation(label, vn)
	}

	if m.version.IsZero() {
		m.version, m.versionAt = v, at
		return nil
	}
	if !m.version.CompatibleWith(v, m.opts.AllowVersionSkew) {
		return newError("version", ErrVersionMismatch, Diagnostic{
			Code: CodeVersionMismatch,
			Message: fmt.Sprintf("cannot merge %s with %s",
				m.version.String(), v.String()),
			At:      at,
			Related: []Location{m.versionAt},
		})
	}
	if m.version.Minor != v.Minor {
		m.warn(Diagnostic{
			Code:    CodeVersionSkew,
			Message: fmt.Sprintf("merging %s with %s", m.version.String(), v.String()),
			At:      at,
			Related: []Location{m.versionAt},
		})
	}
	// Keep the newest patch so the result does not understate what it uses.
	if isBase || m.version.Precedes(v) {
		m.version, m.versionAt = v, at
	}
	return nil
}

func isMissingVersionKey(root *yaml.Node) bool {
	_, hasOpenAPI := yamlx.MapValue(root, "openapi")
	_, hasSwagger := yamlx.MapValue(root, "swagger")
	return !hasOpenAPI && !hasSwagger
}

// Result finalises the merge: it stamps the version, validates local $refs and
// optionally applies canonical ordering.
func (m *Merger) Result() (*Result, error) {
	if m.err != nil {
		return nil, m.err
	}
	if m.result != nil {
		return m.result, nil
	}
	if len(m.sources) == 0 {
		return nil, newError("result", ErrNoInput, Diagnostic{
			Code:    CodeEmptyDocument,
			Message: "nothing to merge",
		})
	}
	if m.opts.Base != "" && !m.baseFound {
		m.warn(Diagnostic{
			Code:    CodeBaseOverride,
			Message: fmt.Sprintf("base %q was never merged; the first input was used instead", m.opts.Base),
		})
	}

	if m.version.IsZero() {
		return nil, newError("result", ErrInvalidDocument, Diagnostic{
			Code:    CodeInvalidDocumentCode,
			Message: "no input declared an openapi or swagger version",
		})
	}
	m.stampVersion()

	if !m.opts.SkipRefValidation {
		if err := m.validateRefs(); err != nil {
			return nil, err
		}
	}
	if m.opts.SortKeys {
		sortDocument(m.root, m.version.Family)
	}
	if err := m.enforceStrict(); err != nil {
		return nil, err
	}

	m.result = &Result{
		Document:    &Document{node: m.root, version: m.version},
		Conflicts:   m.conflicts,
		Diagnostics: m.diags,
		Sources:     m.sources,
	}
	return m.result, nil
}

// stampVersion writes the resolved version key, moving it to the front so the
// output reads like a spec rather than like a merge artefact.
func (m *Merger) stampVersion() {
	key := m.version.Family.RootKey()
	yamlx.MapDelete(m.root, "openapi")
	yamlx.MapDelete(m.root, "swagger")

	kn := yamlx.NewScalar(key)
	vn := &yaml.Node{Kind: yaml.ScalarNode, Tag: yamlx.TagStr, Value: m.version.Raw}
	if m.version.Family == FamilySwagger2 {
		vn.Style = yaml.DoubleQuotedStyle // "2.0" must not decode as a float
	}
	m.root.Content = append([]*yaml.Node{kn, vn}, m.root.Content...)
}

// enforceStrict promotes warnings to errors. Conflict warnings are exempt:
// they only exist because the caller explicitly asked for first/last-wins, so
// promoting them would contradict that choice.
func (m *Merger) enforceStrict() error {
	if !m.opts.Strict {
		return nil
	}
	for _, d := range m.diags {
		if d.Severity != SeverityWarning || d.Code == CodeConflict {
			continue
		}
		return newError("strict", ErrStrict, d)
	}
	return nil
}

func (m *Merger) warn(d Diagnostic) {
	d.Severity = SeverityWarning
	m.diags = append(m.diags, d)
	if m.opts.Reporter != nil {
		m.opts.Reporter.Report(d)
	}
}

func (m *Merger) recordConflict(c Conflict, d Diagnostic) {
	m.conflicts = append(m.conflicts, c)
	m.warn(d)
}
