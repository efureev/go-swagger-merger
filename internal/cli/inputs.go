package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// inputArg is one input named on the command line: a document, or with
// --files-from a file listing documents.
type inputArg struct {
	path string
	list bool
}

type inputArgs []inputArg

// inputFlag lets -i and --files-from append to one list, so that documents
// keep the order they were named in however they were named.
type inputFlag struct {
	args *inputArgs
	list bool
}

func (f inputFlag) String() string {
	if f.args == nil {
		return ""
	}
	paths := make([]string, 0, len(*f.args))
	for _, a := range *f.args {
		if a.list == f.list {
			paths = append(paths, a.path)
		}
	}
	return strings.Join(paths, ",")
}

func (f inputFlag) Set(v string) error {
	*f.args = append(*f.args, inputArg{path: v, list: f.list})
	return nil
}

// listedPath is one document named by a file list, with the line naming it.
type listedPath struct {
	path string
	line int
}

// parseFileList reads a file list: one path per line, resolved against dir.
// A "#" at the start of a line or after a space or tab starts a comment, so a
// file called a#b.yml still works. Surrounding blanks are trimmed and empty
// lines skipped.
func parseFileList(r io.Reader, name, dir string) ([]listedPath, error) {
	var out []listedPath
	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if n == 1 {
			line = strings.TrimPrefix(line, "\ufeff")
		}
		entry := strings.TrimSpace(stripComment(line))
		switch entry {
		case "":
			continue
		case "-":
			return nil, fmt.Errorf("%s:%d: a file list cannot name standard input", name, n)
		}
		path := filepath.FromSlash(entry)
		if !filepath.IsAbs(path) {
			path = filepath.Join(dir, path)
		}
		out = append(out, listedPath{path: path, line: n})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", name, err)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s lists no documents", name)
	}
	return out, nil
}

func stripComment(line string) string {
	for i := 0; i < len(line); i++ {
		if line[i] == '#' && (i == 0 || line[i-1] == ' ' || line[i-1] == '\t') {
			return line[:i]
		}
	}
	return line
}

// errStdinTwice reports a second use of standard input, which can be read
// only once.
var errStdinTwice = errors.New("standard input can be read only once")

// expandInputs replaces each file list with the documents it names.
//
// Every listed document is checked for existence here, so that a stale list
// is reported against the line to fix rather than as an unreadable input. A
// list read from standard input resolves against the working directory.
func expandInputs(args inputArgs, stdin io.Reader) ([]string, error) {
	stdinUses := 0
	for _, a := range args {
		if a.path == "-" {
			stdinUses++
		}
	}
	if stdinUses > 1 {
		return nil, errStdinTwice
	}

	var out []string
	for _, a := range args {
		if !a.list {
			out = append(out, a.path)
			continue
		}

		name, listed, err := readFileList(a.path, stdin)
		if err != nil {
			return nil, err
		}
		for _, l := range listed {
			if _, err := os.Stat(l.path); err != nil {
				return nil, fmt.Errorf("%s:%d: %w", name, l.line, err)
			}
			out = append(out, l.path)
		}
	}
	return out, nil
}

// readFileList reads the list at path, or from stdin for "-", and returns the
// name to report it by.
func readFileList(path string, stdin io.Reader) (string, []listedPath, error) {
	if path == "-" {
		listed, err := parseFileList(stdin, "<stdin>", ".")
		return "<stdin>", listed, err
	}
	f, err := os.Open(path)
	if err != nil {
		return path, nil, fmt.Errorf("cannot read file list: %w", err)
	}
	defer f.Close()
	listed, err := parseFileList(f, path, filepath.Dir(path))
	return path, listed, err
}

// collectInputs gathers the documents a command names, positional inputs
// last, and reports a failure as the exit code to return.
func collectInputs(args inputArgs, fs *flag.FlagSet, stdio IO) ([]string, int) {
	for _, p := range fs.Args() {
		args = append(args, inputArg{path: p})
	}
	inputs, err := expandInputs(args, stdio.In)
	if err != nil {
		fmt.Fprintf(stdio.Err, "error: %s\n", err)
		return nil, ExitFailure
	}
	if len(inputs) == 0 {
		fmt.Fprintln(stdio.Err, "error: no input documents")
		fs.Usage()
		return nil, ExitUsage
	}
	return inputs, ExitOK
}

// resolveBase matches --base to an input by cleaned path when it does not
// match one literally, since a document from a file list is labelled with its
// resolved path: ./docs/users.yml must still find docs/users.yml.
func resolveBase(base string, inputs []string) string {
	if base == "" {
		return base
	}
	want := filepath.Clean(base)
	for _, in := range inputs {
		if in == base {
			return base
		}
	}
	for _, in := range inputs {
		if in != "-" && filepath.Clean(in) == want {
			return in
		}
	}
	return base
}
