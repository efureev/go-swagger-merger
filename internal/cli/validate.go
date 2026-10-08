package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/efureev/go-swagger-merger/v2/merge"
)

type validateConfig struct {
	inputs     inputArgs
	onConflict string
	base       string
	allowSkew  bool
	strict     bool
	allowEmpty bool
	unused     bool
	logFormat  string
}

func validateFlags(cfg *validateConfig, w io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(w)

	fs.Var(inputFlag{args: &cfg.inputs}, "i", "Input file (repeatable; positional arguments work too)")
	fs.Var(inputFlag{args: &cfg.inputs, list: true}, "files-from", "File listing inputs, one per line, relative to it (repeatable)")
	fs.StringVar(&cfg.onConflict, "on-conflict", "error", "How to resolve conflicts: error, first or last")
	fs.StringVar(&cfg.base, "base", "", "Input whose info block wins (default: the first input)")
	fs.BoolVar(&cfg.allowSkew, "allow-version-skew", false, "Allow merging 3.0.x with 3.1.x")
	fs.BoolVar(&cfg.strict, "strict", false, "Treat warnings as errors")
	fs.BoolVar(&cfg.allowEmpty, "allow-empty", false, "Skip empty inputs instead of failing")
	fs.BoolVar(&cfg.unused, "unused", true, "Report components that no $ref points at")
	fs.StringVar(&cfg.logFormat, "log-format", "text", "Diagnostic format: text or json")

	alias(fs, "input", "i")

	fs.Usage = func() {
		fmt.Fprint(w, `Merge documents in memory and report every problem found.

Nothing is written. Use this in CI to fail a build on unresolved references,
conflicting definitions or incompatible spec versions.

Usage:
  swagger-merger validate [flags] <input...>

--files-from names a file listing inputs, one per line, resolved against
the file's own directory; "#" at the start of a line or after a space starts
a comment, and "-" reads the list from standard input. Inputs keep the order
they are named in, whether by -i, --files-from or position.

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprint(w, `
Exit codes:
  0  no problems
  1  the document is unusable, or --strict and a warning was raised
  2  the command line was wrong
`)
	}
	return fs
}

func runValidate(ctx context.Context, args []string, stdio IO) int {
	cfg := &validateConfig{}
	fs := validateFlags(cfg, stdio.Err)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitUsage
	}

	inputs, code := collectInputs(cfg.inputs, fs, stdio)
	if code != ExitOK {
		return code
	}

	policy, err := merge.ParseConflictPolicy(cfg.onConflict)
	if err != nil {
		fmt.Fprintf(stdio.Err, "error: %s\n", err)
		return ExitUsage
	}
	if cfg.logFormat != "text" && cfg.logFormat != "json" {
		fmt.Fprintf(stdio.Err, "error: unknown log format %q (want text or json)\n", cfg.logFormat)
		return ExitUsage
	}

	opts := merge.Options{
		OnConflict:             policy,
		Base:                   resolveBase(cfg.base, inputs),
		AllowVersionSkew:       cfg.allowSkew,
		Strict:                 cfg.strict,
		AllowEmptyDocuments:    cfg.allowEmpty,
		ReportUnusedComponents: cfg.unused,
		Reporter:               reporter{w: stdio.Out, json: cfg.logFormat == "json"},
	}

	res, err := merge.Merge(ctx, opts, sourcesFor(inputs, stdio.In)...)
	if err != nil {
		reportError(stdio.Err, err)
		return ExitFailure
	}

	warnings := len(res.Warnings())
	if warnings == 0 {
		fmt.Fprintf(stdio.Out, "ok: %s, no problems found\n",
			plural(len(res.Sources), "document", "documents"))
		return ExitOK
	}
	fmt.Fprintf(stdio.Out, "%s checked, %s\n",
		plural(len(res.Sources), "document", "documents"),
		plural(warnings, "warning", "warnings"))
	return ExitOK
}
