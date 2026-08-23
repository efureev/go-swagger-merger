package merge_test

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/merge"
)

var update = flag.Bool("update", false, "rewrite golden files from the current output")

const goldenRoot = "../testdata/merge"

// caseOptions is the on-disk form of merge.Options, so a fixture can describe
// the configuration it needs without any Go code.
type caseOptions struct {
	OnConflict        string            `yaml:"onConflict"`
	SectionPolicy     map[string]string `yaml:"sectionPolicy"`
	Base              string            `yaml:"base"`
	SortKeys          bool              `yaml:"sortKeys"`
	SkipRefValidation bool              `yaml:"skipRefValidation"`
	AllowVersionSkew  bool              `yaml:"allowVersionSkew"`
	Strict            bool              `yaml:"strict"`
	AllowEmpty        bool              `yaml:"allowEmpty"`
	ReportUnused      bool              `yaml:"reportUnusedComponents"`
	Format            string            `yaml:"format"`
	Indent            int               `yaml:"indent"`
}

func (co caseOptions) toOptions(t *testing.T) merge.Options {
	t.Helper()
	policy, err := merge.ParseConflictPolicy(co.OnConflict)
	if err != nil {
		t.Fatalf("opts.yaml: %v", err)
	}
	var sections map[merge.Section]merge.ConflictPolicy
	if len(co.SectionPolicy) > 0 {
		sections = make(map[merge.Section]merge.ConflictPolicy, len(co.SectionPolicy))
		// Sorted for reproducibility, though map order does not reach output.
		keys := make([]string, 0, len(co.SectionPolicy))
		for k := range co.SectionPolicy {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p, err := merge.ParseConflictPolicy(co.SectionPolicy[k])
			if err != nil {
				t.Fatalf("opts.yaml sectionPolicy[%s]: %v", k, err)
			}
			sections[merge.Section(k)] = p
		}
	}
	return merge.Options{
		OnConflict:             policy,
		SectionPolicy:          sections,
		Base:                   co.Base,
		SortKeys:               co.SortKeys,
		SkipRefValidation:      co.SkipRefValidation,
		AllowVersionSkew:       co.AllowVersionSkew,
		Strict:                 co.Strict,
		AllowEmptyDocuments:    co.AllowEmpty,
		ReportUnusedComponents: co.ReportUnused,
	}
}

func loadCase(t *testing.T, dir string) (merge.Options, caseOptions, []merge.Source) {
	t.Helper()

	var co caseOptions
	if data, err := os.ReadFile(filepath.Join(dir, "opts.yaml")); err == nil {
		if err := yaml.Unmarshal(data, &co); err != nil {
			t.Fatalf("opts.yaml: %v", err)
		}
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var inputs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "in-") {
			inputs = append(inputs, e.Name())
		}
	}
	if len(inputs) == 0 {
		t.Fatalf("%s has no in-* inputs", dir)
	}
	// Filename order is merge order, which keeps the fixture self-describing.
	sort.Strings(inputs)

	srcs := make([]merge.Source, len(inputs))
	for i, name := range inputs {
		// Name is the bare filename so golden diagnostics do not embed the
		// checkout path.
		srcs[i] = merge.Source{Name: name, Path: filepath.Join(dir, name)}
	}
	return co.toOptions(t), co, srcs
}

func TestGolden(t *testing.T) {
	dirs, err := filepath.Glob(filepath.Join(goldenRoot, "*"))
	if err != nil {
		t.Fatal(err)
	}
	if len(dirs) == 0 {
		t.Fatalf("no fixtures under %s", goldenRoot)
	}

	for _, dir := range dirs {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		t.Run(filepath.Base(dir), func(t *testing.T) {
			runGoldenCase(t, dir)
		})
	}
}

func runGoldenCase(t *testing.T, dir string) {
	t.Helper()
	opts, co, srcs := loadCase(t, dir)

	var diags []merge.Diagnostic
	opts.Reporter = merge.ReporterFunc(func(d merge.Diagnostic) { diags = append(diags, d) })

	res, err := merge.Merge(context.Background(), opts, srcs...)

	errPath := filepath.Join(dir, "want-error.txt")
	wantErr, hasErrGolden := readGolden(t, errPath)

	if hasErrGolden || (err != nil && *update) {
		if err == nil {
			t.Fatalf("expected an error containing %q, got none", strings.TrimSpace(wantErr))
		}
		got := normalize(err.Error()) + "\n"
		if *update {
			writeGolden(t, errPath, got)
			return
		}
		if !strings.Contains(got, strings.TrimSpace(wantErr)) {
			t.Errorf("error mismatch\n--- got ---\n%s\n--- want to contain ---\n%s", got, wantErr)
		}
		return
	}
	if err != nil {
		t.Fatalf("merge failed: %v", err)
	}

	format := co.Format
	if format == "" {
		format = "yaml"
	}
	indent := co.Indent
	if indent == 0 {
		indent = merge.DefaultIndent
	}

	var out []byte
	if format == "json" {
		out, err = res.Document.JSON(indent)
	} else {
		out, err = res.Document.YAML(indent)
	}
	if err != nil {
		t.Fatalf("encode: %v", err)
	}

	wantPath := filepath.Join(dir, "want."+format)
	if *update {
		writeGolden(t, wantPath, string(out))
	} else {
		want, ok := readGolden(t, wantPath)
		if !ok {
			t.Fatalf("%s is missing; run: go test ./merge -run TestGolden -update", wantPath)
		}
		if string(out) != want {
			t.Errorf("output mismatch\n--- got ---\n%s\n--- want ---\n%s", out, want)
		}
	}

	// Diagnostics are golden only where the fixture asks for it, so cases
	// that do not care stay short.
	diagPath := filepath.Join(dir, "want-diags.txt")
	gotDiags := formatDiags(diags)
	if *update {
		if _, ok := readGolden(t, diagPath); ok || gotDiags != "" {
			writeGolden(t, diagPath, gotDiags)
		}
		return
	}
	if want, ok := readGolden(t, diagPath); ok && gotDiags != want {
		t.Errorf("diagnostics mismatch\n--- got ---\n%s\n--- want ---\n%s", gotDiags, want)
	}
}

func formatDiags(diags []merge.Diagnostic) string {
	if len(diags) == 0 {
		return ""
	}
	var sb strings.Builder
	for _, d := range diags {
		sb.WriteString(normalize(d.String()))
		sb.WriteString("\n")
	}
	return sb.String()
}

// normalize strips the fixture directory from paths so goldens do not depend
// on where the repository is checked out.
func normalize(s string) string {
	s = strings.ReplaceAll(s, goldenRoot+string(filepath.Separator), "")
	return strings.ReplaceAll(s, goldenRoot+"/", "")
}

func readGolden(t *testing.T, path string) (string, bool) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	return string(data), true
}

func writeGolden(t *testing.T, path, content string) {
	t.Helper()
	if content == "" {
		_ = os.Remove(path)
		return
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("updated %s", path)
}
