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

	// seqIndexes holds the dedup index of every nested sequence, keyed by its
	// pointer. Keeping them across Add calls is what lets a conflict name the
	// file an existing element actually came from; rebuilding the index from
	// the destination each time would attribute it to whoever is being merged
	// right now.
	seqIndexes map[string]seqIndex

	// pathTemplates maps a path with its parameter names erased to the first
	// spelling seen, which is how equivalent-but-differently-named templates
	// are detected.
	pathTemplates map[string]string

	// origin records where the definition currently held at a pointer came
	// from, so a later conflict can name both sides.
	origin map[string]Location

	// baseWarned keeps the base-wins notice to one per key.
	baseWarned map[string]bool

	diags     []Diagnostic
	conflicts []Conflict

	// finalDiags holds the diagnostics produced by Result rather than by
	// merging. They are discarded and recomputed on every Result so that
	// Add/Result/Add/Result does not report the same problem twice.
	finalDiags []Diagnostic
	finalizing bool

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
	m := &Merger{opts: opts}
	m.init()
	return m
}

// init prepares the lazily-created state. It runs from New and again from
// every entry point, so that a zero-value Merger works instead of panicking on
// a nil map -- an exported type must not have an invalid zero value when the
// package promises never to panic.
func (m *Merger) init() {
	if m.root == nil {
		m.root = yamlx.NewMapping()
	}
	if m.servers == nil {
		m.servers = seqIndex{}
	}
	if m.tags == nil {
		m.tags = seqIndex{}
	}
	if m.security == nil {
		m.security = seqIndex{}
	}
	if m.seqIndexes == nil {
		m.seqIndexes = map[string]seqIndex{}
	}
	if m.pathTemplates == nil {
		m.pathTemplates = map[string]string{}
	}
	if m.origin == nil {
		m.origin = map[string]Location{}
	}
	if m.baseWarned == nil {
		m.baseWarned = map[string]bool{}
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
	m.init()
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
	m.init()
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

	// Recomputed from scratch: Result may run again after another Add.
	m.finalDiags = nil
	m.finalizing = true
	defer func() { m.finalizing = false }()

	// One pass feeds both consumers, which are independently switchable.
	scan := m.scanRefs()
	if !m.opts.SkipRefValidation {
		if err := m.validateRefs(scan.refs, scan.siblings); err != nil {
			return nil, err
		}
	}
	if m.opts.ReportUnusedComponents {
		m.reportUnusedComponents(scan.refs)
	}

	if m.opts.SortKeys {
		sortDocument(m.root, m.version.Family)
	}

	diags := make([]Diagnostic, 0, len(m.diags)+len(m.finalDiags))
	diags = append(diags, m.diags...)
	diags = append(diags, m.finalDiags...)

	if err := enforceStrict(m.opts.Strict, diags); err != nil {
		return nil, err
	}

	m.result = &Result{
		Document:    &Document{node: m.root, version: m.version},
		Conflicts:   m.conflicts,
		Diagnostics: diags,
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

// strictExempt lists the warnings that --strict must not promote, because
// neither is a defect the caller can act on.
//
// A conflict warning exists only because the caller asked for first/last-wins,
// so promoting it would contradict that choice. A base-override warning is
// unavoidable: every input carries its own info block, so promoting it would
// make --strict fail on every ordinary multi-document merge.
var strictExempt = map[string]bool{
	CodeConflict:     true,
	CodeBaseOverride: true,
}

// enforceStrict promotes warnings to errors.
func enforceStrict(strict bool, diags []Diagnostic) error {
	if !strict {
		return nil
	}
	for _, d := range diags {
		if d.Severity != SeverityWarning || strictExempt[d.Code] {
			continue
		}
		return newError("strict", ErrStrict, d)
	}
	return nil
}

func (m *Merger) warn(d Diagnostic) {
	d.Severity = SeverityWarning
	if m.finalizing {
		m.finalDiags = append(m.finalDiags, d)
	} else {
		m.diags = append(m.diags, d)
	}
	if m.opts.Reporter != nil {
		m.opts.Reporter.Report(d)
	}
}

func (m *Merger) recordConflict(c Conflict, d Diagnostic) {
	m.conflicts = append(m.conflicts, c)
	m.warn(d)
}
