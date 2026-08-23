package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
	"github.com/efureev/go-swagger-merger/v2/merge"
)

// resolveFormat picks the output encoding, inferring it from the output file
// extension when it was not stated.
func resolveFormat(explicit, outPath string) (string, error) {
	switch explicit {
	case "yaml", "yml":
		return "yaml", nil
	case "json":
		return "json", nil
	case "":
	default:
		return "", fmt.Errorf("unknown format %q (want yaml or json)", explicit)
	}
	if strings.EqualFold(filepath.Ext(outPath), ".json") {
		return "json", nil
	}
	return "yaml", nil
}

func render(doc *merge.Document, format string, indent int) ([]byte, error) {
	if format == "json" {
		return doc.JSON(indent)
	}
	return doc.YAML(indent)
}

// writeOutput sends data to a file or, for "-" and the empty path, to stdout.
func writeOutput(path string, data []byte, stdout io.Writer) error {
	if path == "" || path == "-" {
		_, err := stdout.Write(data)
		return err
	}
	return atomicWrite(path, data)
}

// atomicWrite replaces path in one step.
//
// Writing in place would truncate the destination before the merge is known to
// have succeeded, and the destination is very often one of the inputs -- the
// documented usage merges a file back over itself. A rename cannot leave a
// half-written spec behind.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}

	tmp, err := os.CreateTemp(dir, ".swagger-merger-*")
	if err != nil {
		return fmt.Errorf("cannot create a temporary file in %s: %w", dir, err)
	}
	tmpName := tmp.Name()

	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		cleanup()
		return err
	}
	// Close is checked: a deferred close would swallow a flush failure and
	// report success for a truncated file.
	if err := tmp.Close(); err != nil {
		cleanup()
		return err
	}
	if err := os.Chmod(tmpName, mode); err != nil {
		cleanup()
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return err
	}
	return nil
}

// outputCollidesWithInput reports whether the destination is also an input.
// The write is atomic either way, so this is a note rather than a refusal.
func outputCollidesWithInput(output string, inputs []string) (string, bool) {
	if output == "" || output == "-" {
		return "", false
	}
	outInfo, err := os.Stat(output)
	outAbs, absErr := filepath.Abs(output)

	for _, in := range inputs {
		if in == "-" {
			continue
		}
		if absErr == nil {
			if inAbs, err := filepath.Abs(in); err == nil && inAbs == outAbs {
				return in, true
			}
		}
		if err == nil {
			if inInfo, statErr := os.Stat(in); statErr == nil && os.SameFile(outInfo, inInfo) {
				return in, true
			}
		}
	}
	return "", false
}

// countMembers returns the size of a mapping at pointer, for the summary line.
func countMembers(doc *merge.Document, pointer string) int {
	node, ok := yamlx.At(doc.Node(), pointer)
	if !ok || !yamlx.IsMapping(node) {
		return 0
	}
	return len(yamlx.Entries(node))
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}

func diagnosticJSON(d merge.Diagnostic) string {
	type location struct {
		Source string `json:"source,omitempty"`
		Line   int    `json:"line,omitempty"`
		Column int    `json:"column,omitempty"`
	}
	payload := struct {
		Severity string     `json:"severity"`
		Code     string     `json:"code,omitempty"`
		Message  string     `json:"message"`
		Pointer  string     `json:"pointer,omitempty"`
		At       location   `json:"at,omitempty"`
		Related  []location `json:"related,omitempty"`
	}{
		Severity: d.Severity.String(),
		Code:     d.Code,
		Message:  d.Message,
		Pointer:  d.Pointer,
		At:       location{d.At.Source, d.At.Line, d.At.Column},
	}
	for _, r := range d.Related {
		payload.Related = append(payload.Related, location{r.Source, r.Line, r.Column})
	}
	out, err := json.Marshal(payload)
	if err != nil {
		return fmt.Sprintf(`{"severity":%q,"message":%q}`, d.Severity.String(), d.Message)
	}
	return string(out)
}
