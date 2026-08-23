package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/efureev/go-swagger-merger/v2/merge"
)

type mergeConfig struct {
	output          string
	inputs          stringList
	format          string
	indent          int
	onConflict      string
	sectionPolicy   stringList
	base            string
	sortKeys        bool
	noRefValidation bool
	allowSkew       bool
	strict          bool
	allowEmpty      bool
	dryRun          bool
	quiet           bool
	logFormat       string
}

func mergeFlags(cfg *mergeConfig, w io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("merge", flag.ContinueOnError)
	fs.SetOutput(w)

	fs.StringVar(&cfg.output, "o", "-", `Output file, or "-" for stdout`)
	fs.Var(&cfg.inputs, "i", "Input file (repeatable; positional arguments work too)")
	fs.StringVar(&cfg.format, "format", "", "Output format: yaml or json (default: inferred from -o)")
	fs.IntVar(&cfg.indent, "indent", merge.DefaultIndent, "Indentation width")
	fs.StringVar(&cfg.onConflict, "on-conflict", "error", "How to resolve conflicts: error, first or last")
	fs.Var(&cfg.sectionPolicy, "on-conflict-section", "Per-section policy, e.g. schemas=first (repeatable)")
	fs.StringVar(&cfg.base, "base", "", "Input whose info block and version win (default: the first input)")
	fs.BoolVar(&cfg.sortKeys, "sort", false, "Emit keys in canonical order instead of input order")
	fs.BoolVar(&cfg.noRefValidation, "no-ref-validation", false, "Do not check that local $refs resolve")
	fs.BoolVar(&cfg.allowSkew, "allow-version-skew", false, "Allow merging 3.0.x with 3.1.x")
	fs.BoolVar(&cfg.strict, "strict", false, "Treat warnings as errors")
	fs.BoolVar(&cfg.allowEmpty, "allow-empty", false, "Skip empty inputs instead of failing")
	fs.BoolVar(&cfg.dryRun, "dry-run", false, "Merge and report, but write nothing")
	fs.BoolVar(&cfg.quiet, "q", false, "Suppress diagnostics and the summary")
	fs.StringVar(&cfg.logFormat, "log-format", "text", "Diagnostic format: text or json")

	alias(fs, "output", "o")
	alias(fs, "input", "i")
	alias(fs, "quiet", "q")

	fs.Usage = func() {
		fmt.Fprint(w, `Merge OpenAPI or Swagger documents into one.

Usage:
  swagger-merger merge [flags] <input...>

Inputs are merged in order. The first supplies the info block and the spec
version unless --base names another. "-" reads standard input.

Flags:
`)
		fs.PrintDefaults()
		fmt.Fprint(w, `
Exit codes:
  0  merged
  1  the merge failed
  2  the command line was wrong

Examples:
  swagger-merger merge -o docs/swagger.yml docs/users.yml docs/orders.yml
  swagger-merger merge --on-conflict=first -o api.json a.yaml b.yaml
  cat spec.yaml | swagger-merger merge - extra.yaml
`)
	}
	return fs
}

func (cfg *mergeConfig) options(diagnostics io.Writer) (merge.Options, error) {
	policy, err := merge.ParseConflictPolicy(cfg.onConflict)
	if err != nil {
		return merge.Options{}, err
	}
	sections, err := parseSectionPolicies(cfg.sectionPolicy)
	if err != nil {
		return merge.Options{}, err
	}
	if cfg.logFormat != "text" && cfg.logFormat != "json" {
		return merge.Options{}, fmt.Errorf("unknown log format %q (want text or json)", cfg.logFormat)
	}

	opts := merge.Options{
		OnConflict:          policy,
		SectionPolicy:       sections,
		Base:                cfg.base,
		SortKeys:            cfg.sortKeys,
		SkipRefValidation:   cfg.noRefValidation,
		AllowVersionSkew:    cfg.allowSkew,
		Strict:              cfg.strict,
		AllowEmptyDocuments: cfg.allowEmpty,
	}
	if !cfg.quiet {
		opts.Reporter = reporter{w: diagnostics, json: cfg.logFormat == "json"}
	}
	return opts, nil
}

func runMerge(ctx context.Context, args []string, stdio IO) int {
	cfg := &mergeConfig{}
	fs := mergeFlags(cfg, stdio.Err)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return ExitOK
		}
		return ExitUsage
	}

	inputs := append([]string(cfg.inputs), fs.Args()...)
	if len(inputs) == 0 {
		fmt.Fprintln(stdio.Err, "error: no input documents")
		fs.Usage()
		return ExitUsage
	}

	opts, err := cfg.options(stdio.Err)
	if err != nil {
		fmt.Fprintf(stdio.Err, "error: %s\n", err)
		return ExitUsage
	}
	format, err := resolveFormat(cfg.format, cfg.output)
	if err != nil {
		fmt.Fprintf(stdio.Err, "error: %s\n", err)
		return ExitUsage
	}

	res, err := merge.Merge(ctx, opts, sourcesFor(inputs, stdio.In)...)
	if err != nil {
		reportError(stdio.Err, err)
		return ExitFailure
	}

	data, err := render(res.Document, format, cfg.indent)
	if err != nil {
		fmt.Fprintf(stdio.Err, "error: cannot encode the merged document: %s\n", err)
		return ExitFailure
	}

	if cfg.dryRun {
		if !cfg.quiet {
			fmt.Fprintf(stdio.Err, "%s (dry run, nothing written)\n", summary(res, cfg.output))
		}
		return ExitOK
	}

	if clash, ok := outputCollidesWithInput(cfg.output, inputs); ok && !cfg.quiet {
		reporter{w: stdio.Err, json: cfg.logFormat == "json"}.Report(merge.Diagnostic{
			Severity: merge.SeverityWarning,
			Code:     merge.CodeOutputIsInput,
			Message: fmt.Sprintf("the output %s is also an input (%s); it is replaced atomically",
				cfg.output, clash),
		})
	}

	if err := writeOutput(cfg.output, data, stdio.Out); err != nil {
		fmt.Fprintf(stdio.Err, "error: cannot write %s: %s\n", cfg.output, err)
		return ExitFailure
	}
	if !cfg.quiet {
		fmt.Fprintln(stdio.Err, summary(res, cfg.output))
	}
	return ExitOK
}

func summary(res *merge.Result, output string) string {
	dest := output
	if dest == "" || dest == "-" {
		dest = "stdout"
	}
	parts := fmt.Sprintf("merged %s into %s: %s",
		plural(len(res.Sources), "document", "documents"), dest,
		plural(countMembers(res.Document, "/paths"), "path", "paths"))

	if n := countMembers(res.Document, "/components/schemas") + countMembers(res.Document, "/definitions"); n > 0 {
		parts += ", " + plural(n, "schema", "schemas")
	}
	if n := len(res.Conflicts); n > 0 {
		parts += ", " + plural(n, "conflict", "conflicts")
	}
	if n := len(res.Warnings()); n > 0 {
		parts += ", " + plural(n, "warning", "warnings")
	}
	return parts
}
