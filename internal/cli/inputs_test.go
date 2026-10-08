package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseFileList(t *testing.T) {
	const list = "\ufeff# the base comes first\r\n" +
		"base.yml\r\n" +
		"\r\n" +
		"   users/users.yml\t# users\r\n" +
		"a#b.yml\n" +
		"  # indented comment\n" +
		"../shared/errors.yml#not-a-comment\n"

	listed, err := parseFileList(strings.NewReader(list), "specs/swagger.list", "specs")
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(listed))
	for i, l := range listed {
		got[i] = l.path
	}
	want := []string{
		filepath.Join("specs", "base.yml"),
		filepath.Join("specs", "users", "users.yml"),
		filepath.Join("specs", "a#b.yml"),
		filepath.Join("shared", "errors.yml#not-a-comment"),
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

func TestParseFileListKeepsAbsolutePaths(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "a.yml")
	got, err := parseFileList(strings.NewReader(abs+"\n"), "l.list", "elsewhere")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].path != abs {
		t.Errorf("got %v, want [%q]", got, abs)
	}
}

func TestParseFileListRejects(t *testing.T) {
	cases := map[string]struct{ list, want string }{
		"standard input": {"a.yml\n-\n", "l.list:2"},
		"no entries":     {"# all of it\n\n  # commented out\n", "lists no documents"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := parseFileList(strings.NewReader(tc.list), "l.list", ".")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// -i and --files-from share one list, so the documents keep the order they
// were named in; positional inputs follow.
func TestExpandInputsKeepsCommandLineOrder(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.yaml", specA)
	write(t, dir, "b.yaml", specB)
	list := write(t, dir, "m.list", "a.yaml\nb.yaml\n")

	got, err := expandInputs(inputArgs{{path: "x.yaml"}, {path: list, list: true}, {path: "y.yaml"}}, strings.NewReader(""))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"x.yaml", filepath.Join(dir, "a.yaml"), filepath.Join(dir, "b.yaml"), "y.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got  %q\nwant %q", got, want)
	}
}

// A stale list is fixed in the list, so the error must point at the line.
func TestExpandInputsNamesTheLineOfAMissingDocument(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.yaml", specA)
	list := write(t, dir, "swagger.list", "a.yaml\ngone.yaml\n")

	_, err := expandInputs(inputArgs{{path: list, list: true}}, strings.NewReader(""))
	if err == nil || !strings.Contains(err.Error(), "swagger.list:2") {
		t.Errorf("err = %v, want it to name swagger.list:2", err)
	}
}

func TestExpandInputsReadsStandardInputOnce(t *testing.T) {
	for name, args := range map[string]inputArgs{
		"two lists":          {{path: "-", list: true}, {path: "-", list: true}},
		"a list and a input": {{path: "-", list: true}, {path: "-"}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := expandInputs(args, strings.NewReader(""))
			if err == nil || !strings.Contains(err.Error(), "only once") {
				t.Errorf("err = %v, want a refusal to read standard input twice", err)
			}
		})
	}
}

func TestResolveBase(t *testing.T) {
	inputs := []string{filepath.Join("specs", "a.yaml"), filepath.Join("specs", "b.yaml")}
	for base, want := range map[string]string{
		"./specs/a.yaml":                 filepath.Join("specs", "a.yaml"),
		"specs/../specs/b.yaml":          filepath.Join("specs", "b.yaml"),
		filepath.Join("specs", "a.yaml"): filepath.Join("specs", "a.yaml"),
		"other.yaml":                     "other.yaml",
		"":                               "",
	} {
		if got := resolveBase(filepath.FromSlash(base), inputs); got != want {
			t.Errorf("resolveBase(%q) = %q, want %q", base, got, want)
		}
	}
}

// ── Through the command line ────────────────────────────────────────────────

// Entries resolve against the list's own directory, wherever the command runs,
// and the first listed document is the base.
func TestMergeFilesFromResolvesAgainstTheList(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "shared/b.yaml", specB)
	write(t, dir, "specs/a.yaml", specA)
	list := write(t, dir, "specs/swagger.list", "# base first\n../shared/b.yaml  # titled B\na.yaml\n")

	got := exec(t, "", "merge", "--files-from", list)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	for _, want := range []string{"title: B", "get:", "post:"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, got.stdout)
		}
	}
}

func TestMergeFilesFromMissingDocumentExitsOne(t *testing.T) {
	dir := t.TempDir()
	list := write(t, dir, "swagger.list", "gone.yaml\n")

	got := exec(t, "", "merge", "--files-from", list)
	if got.code != ExitFailure {
		t.Fatalf("code = %d, want %d", got.code, ExitFailure)
	}
	if !strings.Contains(got.stderr, "swagger.list:1") {
		t.Errorf("stderr should name the line:\n%s", got.stderr)
	}
}

func TestMergeFilesFromStandardInput(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", specB)

	got := exec(t, a+"\n"+b+"\n", "merge", "--files-from", "-")
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "post:") {
		t.Errorf("the listed documents were not merged:\n%s", got.stdout)
	}
}

func TestValidateFilesFrom(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "a.yaml", specA)
	write(t, dir, "b.yaml", specB)
	list := write(t, dir, "swagger.list", "a.yaml\nb.yaml\n")

	got := exec(t, "", "validate", "--files-from", list)
	if got.code != ExitOK || !strings.Contains(got.stdout, "2 documents") {
		t.Fatalf("code = %d, stdout:\n%s\nstderr:\n%s", got.code, got.stdout, got.stderr)
	}
}

func TestOutputListedAsAnInputIsReported(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	write(t, dir, "b.yaml", specB)
	list := write(t, dir, "swagger.list", "a.yaml\nb.yaml\n")

	got := exec(t, "", "merge", "-o", a, "--files-from", list)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "is also an input") {
		t.Errorf("expected a warning about the output being an input:\n%s", got.stderr)
	}
	if data, _ := os.ReadFile(a); !strings.Contains(string(data), "post:") {
		t.Errorf("merged content did not land in the output:\n%s", data)
	}
}
