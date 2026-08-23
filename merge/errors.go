package merge

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Sentinel errors. Every error returned by this package wraps one of these, so
// callers can branch with errors.Is instead of matching on message text.
var (
	// ErrConflict reports two inputs defining the same thing differently.
	ErrConflict = errors.New("conflicting definition")
	// ErrVersionMismatch reports inputs whose spec versions cannot be merged.
	ErrVersionMismatch = errors.New("incompatible spec versions")
	// ErrDanglingRef reports a local $ref that resolves to nothing.
	ErrDanglingRef = errors.New("unresolved $ref")
	// ErrInvalidDocument reports input that is not a usable OpenAPI document.
	ErrInvalidDocument = errors.New("invalid document")
	// ErrNoInput reports a merge with nothing to merge.
	ErrNoInput = errors.New("no input documents")
	// ErrStrict reports a warning promoted to an error by Options.Strict.
	ErrStrict = errors.New("warning treated as error")
)

// Severity classifies a Diagnostic.
type Severity uint8

// Diagnostic severities.
const (
	SeverityWarning Severity = iota
	SeverityError
)

func (s Severity) String() string {
	if s == SeverityError {
		return "error"
	}
	return "warn"
}

// Diagnostic codes, stable enough to grep for in CI logs.
const (
	CodeConflict             = "conflict"
	CodeDanglingRef          = "dangling-ref"
	CodeExternalRef          = "external-ref"
	CodeRefSiblings          = "ref-siblings"
	CodeVersionMismatch      = "version-mismatch"
	CodeVersionSkew          = "version-skew"
	CodeInvalidServer        = "invalid-server"
	CodeInvalidTag           = "invalid-tag"
	CodeDuplicatePathTmpl    = "duplicate-path-template"
	CodeBaseOverride         = "base-override"
	CodeEmptyDocument        = "empty-document"
	CodeUnusedComponent      = "unused-component"
	CodeInvalidComponentName = "invalid-component-name"
	CodeOutputIsInput        = "output-is-input"
)

// Location points at a node in one of the input files.
type Location struct {
	Source string
	Line   int
	Column int
}

// String renders the location as file:line:col, omitting parts it does not
// have, so it stays clickable in a terminal.
func (l Location) String() string {
	if l.Source == "" {
		return "<unknown>"
	}
	if l.Line <= 0 {
		return l.Source
	}
	if l.Column <= 0 {
		return l.Source + ":" + strconv.Itoa(l.Line)
	}
	return l.Source + ":" + strconv.Itoa(l.Line) + ":" + strconv.Itoa(l.Column)
}

// Conflict records two inputs defining the same key differently, and how the
// merge resolved it.
type Conflict struct {
	Section    Section
	Pointer    string // RFC 6901, e.g. /components/schemas/User
	Kept       Location
	Dropped    Location
	Resolution string // "first-wins", "last-wins" or "error"
}

func (c Conflict) String() string {
	return fmt.Sprintf("%s: %s kept %s, dropped %s (%s)",
		c.Pointer, c.Section, c.Kept, c.Dropped, c.Resolution)
}

// Diagnostic is a single note about the merge.
type Diagnostic struct {
	Severity Severity
	Code     string
	Message  string
	At       Location
	Pointer  string
	Related  []Location
}

func (d Diagnostic) String() string {
	var sb strings.Builder
	sb.WriteString(d.Severity.String())
	sb.WriteString(": ")
	sb.WriteString(d.Message)
	if d.Pointer != "" {
		sb.WriteString("\n  at ")
		sb.WriteString(d.Pointer)
	}
	if d.At.Source != "" {
		sb.WriteString("\n  ")
		sb.WriteString(d.At.String())
	}
	for _, r := range d.Related {
		sb.WriteString("\n  ")
		sb.WriteString(r.String())
	}
	if d.Code != "" {
		sb.WriteString("\n  [")
		sb.WriteString(d.Code)
		sb.WriteString("]")
	}
	return sb.String()
}

// Error carries a Diagnostic alongside the sentinel it wraps.
type Error struct {
	Op   string
	Diag Diagnostic
	Err  error
}

func (e *Error) Error() string {
	var sb strings.Builder
	if e.Op != "" {
		sb.WriteString(e.Op)
		sb.WriteString(": ")
	}
	if e.Err != nil {
		sb.WriteString(e.Err.Error())
	}
	if e.Diag.Message != "" {
		sb.WriteString(": ")
		sb.WriteString(e.Diag.Message)
	}
	if e.Diag.Pointer != "" {
		sb.WriteString("\n  at ")
		sb.WriteString(e.Diag.Pointer)
	}
	if e.Diag.At.Source != "" {
		sb.WriteString("\n  ")
		sb.WriteString(e.Diag.At.String())
	}
	for _, r := range e.Diag.Related {
		sb.WriteString("\n  ")
		sb.WriteString(r.String())
	}
	return sb.String()
}

func (e *Error) Unwrap() error { return e.Err }

func newError(op string, err error, d Diagnostic) *Error {
	d.Severity = SeverityError
	return &Error{Op: op, Diag: d, Err: err}
}
