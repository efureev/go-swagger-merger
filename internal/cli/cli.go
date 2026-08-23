// Package cli implements the swagger-merger command line.
//
// Run returns an exit code instead of calling os.Exit, and writes through an
// injected IO rather than to the process streams, so every command is directly
// testable.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/efureev/go-swagger-merger/v2/merge"
)

// Exit codes.
const (
	ExitOK      = 0
	ExitFailure = 1
	ExitUsage   = 2
)

// IO holds the streams a command reads and writes.
type IO struct {
	In  io.Reader
	Out io.Writer
	Err io.Writer
}

// BuildInfo describes the binary, stamped at link time.
type BuildInfo struct {
	Version string
	Commit  string
	Date    string
}

// Run dispatches a subcommand and returns the process exit code.
func Run(ctx context.Context, args []string, stdio IO, build BuildInfo) int {
	if len(args) == 0 {
		usage(stdio.Err)
		return ExitUsage
	}

	cmd, rest := args[0], args[1:]
	// Help is matched before the compatibility rewrite below, or "--help"
	// would be taken for a v1-style merge invocation.
	switch cmd {
	case "help", "-h", "--help", "-help":
		return runHelp(rest, stdio)
	case "-version", "--version":
		return runVersion(rest, stdio, build)
	}
	// A leading flag otherwise means the v1 invocation style, which had no
	// subcommand: swagger-merger -o out.yml -i a.yml -i b.yml.
	if strings.HasPrefix(cmd, "-") {
		cmd, rest = "merge", args
	}

	switch cmd {
	case "merge":
		return runMerge(ctx, rest, stdio)
	case "validate":
		return runValidate(ctx, rest, stdio)
	case "version":
		return runVersion(rest, stdio, build)
	default:
		fmt.Fprintf(stdio.Err, "unknown command %q\n\n", cmd)
		usage(stdio.Err)
		return ExitUsage
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `swagger-merger merges OpenAPI and Swagger documents into one.

Usage:
  swagger-merger <command> [flags] [inputs...]

Commands:
  merge      Merge documents into a single specification
  validate   Merge in memory and report every problem found
  version    Print version information
  help       Show help for a command

Run "swagger-merger help <command>" for details.
`)
}

func runHelp(args []string, stdio IO) int {
	if len(args) == 0 {
		usage(stdio.Out)
		return ExitOK
	}
	switch args[0] {
	case "merge":
		mergeFlags(&mergeConfig{}, stdio.Out).Usage()
	case "validate":
		validateFlags(&validateConfig{}, stdio.Out).Usage()
	case "version":
		fmt.Fprintln(stdio.Out, "Usage: swagger-merger version")
	default:
		fmt.Fprintf(stdio.Err, "unknown command %q\n", args[0])
		return ExitUsage
	}
	return ExitOK
}

// stringList collects a repeatable flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// alias registers a second spelling for an already-defined flag, which is how
// short and long forms share one variable under the standard flag package.
func alias(fs *flag.FlagSet, name, existing string) {
	f := fs.Lookup(existing)
	if f == nil {
		return
	}
	fs.Var(f.Value, name, "alias for -"+existing)
}

// sourcesFor turns command-line inputs into merge sources, mapping "-" to
// standard input.
func sourcesFor(inputs []string, stdin io.Reader) []merge.Source {
	srcs := make([]merge.Source, 0, len(inputs))
	for _, in := range inputs {
		if in == "-" {
			srcs = append(srcs, merge.ReaderSource("<stdin>", stdin))
			continue
		}
		srcs = append(srcs, merge.FileSource(in))
	}
	return srcs
}

// parseSectionPolicies reads repeated "section=policy" pairs.
func parseSectionPolicies(specs []string) (map[merge.Section]merge.ConflictPolicy, error) {
	if len(specs) == 0 {
		return nil, nil
	}
	out := make(map[merge.Section]merge.ConflictPolicy, len(specs))
	for _, spec := range specs {
		for _, pair := range strings.Split(spec, ",") {
			pair = strings.TrimSpace(pair)
			if pair == "" {
				continue
			}
			name, value, ok := strings.Cut(pair, "=")
			if !ok {
				return nil, fmt.Errorf("expected section=policy, got %q", pair)
			}
			policy, err := merge.ParseConflictPolicy(strings.TrimSpace(value))
			if err != nil {
				return nil, err
			}
			out[merge.Section(strings.TrimSpace(name))] = policy
		}
	}
	return out, nil
}

// reporter streams diagnostics as the merge produces them.
type reporter struct {
	w    io.Writer
	json bool
}

func (r reporter) Report(d merge.Diagnostic) {
	if r.w == nil {
		return
	}
	if r.json {
		fmt.Fprintln(r.w, diagnosticJSON(d))
		return
	}
	fmt.Fprintln(r.w, d.String())
}

// reportError prints a failure, adding a hint for the cases where the fix is
// a flag the user has not met yet.
func reportError(w io.Writer, err error) {
	fmt.Fprintf(w, "error: %s\n", err)

	switch {
	case errors.Is(err, merge.ErrConflict):
		fmt.Fprintln(w, "\nhint: pass --on-conflict=first or --on-conflict=last to pick a winner,")
		fmt.Fprintln(w, "      or --on-conflict-section schemas=first to scope it to one section.")
	case errors.Is(err, merge.ErrVersionMismatch):
		fmt.Fprintln(w, "\nhint: pass --allow-version-skew to merge 3.0 and 3.1 documents.")
	case errors.Is(err, merge.ErrDanglingRef):
		fmt.Fprintln(w, "\nhint: pass --no-ref-validation to keep the reference as-is.")
	case errors.Is(err, merge.ErrStrict):
		fmt.Fprintln(w, "\nhint: drop --strict to keep this as a warning.")
	}
}
