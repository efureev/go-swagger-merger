package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const specA = `openapi: 3.0.3
info: {title: A, version: "1.0"}
paths:
  /users:
    get:
      responses:
        "200": {description: ok}
`

const specB = `openapi: 3.0.3
info: {title: B, version: "1.0"}
paths:
  /users:
    post:
      responses:
        "201": {description: created}
`

const conflicting = `openapi: 3.0.3
info: {title: A, version: "1.0"}
paths:
  /users:
    get:
      responses:
        "200": {description: different}
`

type run struct {
	code   int
	stdout string
	stderr string
}

// exec runs the CLI with in-memory streams, which is the whole reason Run
// returns a code instead of calling os.Exit.
func exec(t *testing.T, stdin string, args ...string) run {
	t.Helper()
	var out, errBuf bytes.Buffer
	code := Run(context.Background(), args,
		IO{In: strings.NewReader(stdin), Out: &out, Err: &errBuf},
		BuildInfo{Version: "test", Commit: "abc123", Date: "2026-01-01"})
	return run{code: code, stdout: out.String(), stderr: errBuf.String()}
}

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMergeToStdout(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", specB)

	got := exec(t, "", "merge", a, b)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	for _, want := range []string{"get:", "post:", "/users:"} {
		if !strings.Contains(got.stdout, want) {
			t.Errorf("stdout missing %q:\n%s", want, got.stdout)
		}
	}
	if !strings.Contains(got.stderr, "merged 2 documents into stdout") {
		t.Errorf("missing summary:\n%s", got.stderr)
	}
}

func TestMergeToFileIsAtomicAndComplete(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", specB)
	out := filepath.Join(dir, "out.yaml")

	if got := exec(t, "", "merge", "-o", out, a, b); got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "post:") {
		t.Errorf("output is incomplete:\n%s", data)
	}
	// The temporary file must not survive the rename.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".swagger-merger-") {
			t.Errorf("left a temporary file behind: %s", e.Name())
		}
	}
}

// A failed merge must not touch an existing output file. Writing in place, as
// the previous implementation did, truncated it before the merge was known to
// succeed.
func TestFailedMergeLeavesOutputUntouched(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", conflicting)
	out := write(t, dir, "out.yaml", "# precious\n")

	if got := exec(t, "", "merge", "-o", out, a, b); got.code != ExitFailure {
		t.Fatalf("code = %d, want %d", got.code, ExitFailure)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# precious\n" {
		t.Errorf("output was modified by a failed merge:\n%s", data)
	}
}

func TestOutputThatIsAlsoAnInput(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", specB)

	got := exec(t, "", "merge", "-o", a, a, b)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stderr, "is also an input") {
		t.Errorf("expected a warning about the output being an input:\n%s", got.stderr)
	}
	data, _ := os.ReadFile(a)
	if !strings.Contains(string(data), "post:") {
		t.Errorf("merged content did not land in the output:\n%s", data)
	}
}

func TestFormatInferredFromExtension(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	out := filepath.Join(dir, "out.json")

	if got := exec(t, "", "merge", "-o", out, a); got.code != ExitOK {
		t.Fatalf("code = %d", got.code)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("output is not JSON: %v\n%s", err, data)
	}
	if doc["openapi"] != "3.0.3" {
		t.Errorf("decoded %v", doc)
	}
}

// The v1 command line took repeated -i flags and no subcommand. Both still work.
func TestV1CompatibleInvocation(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", specB)
	out := filepath.Join(dir, "swag.yaml")

	got := exec(t, "", "-o", out, "-i", a, "-i", b)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	data, _ := os.ReadFile(out)
	if !strings.Contains(string(data), "post:") {
		t.Errorf("v1-style invocation produced:\n%s", data)
	}
}

func TestStdinInput(t *testing.T) {
	dir := t.TempDir()
	b := write(t, dir, "b.yaml", specB)

	got := exec(t, specA, "merge", "-", b)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "get:") || !strings.Contains(got.stdout, "post:") {
		t.Errorf("stdin document was not merged:\n%s", got.stdout)
	}
}

func TestConflictExitsOneWithHint(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", conflicting)

	got := exec(t, "", "merge", a, b)
	if got.code != ExitFailure {
		t.Fatalf("code = %d, want %d", got.code, ExitFailure)
	}
	if !strings.Contains(got.stderr, "conflicting definition") {
		t.Errorf("stderr:\n%s", got.stderr)
	}
	if !strings.Contains(got.stderr, "--on-conflict=first") {
		t.Errorf("expected a hint pointing at the escape hatch:\n%s", got.stderr)
	}
	if got.stdout != "" {
		t.Errorf("a failed merge should write nothing to stdout:\n%s", got.stdout)
	}
}

func TestOnConflictFirstSucceeds(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", conflicting)

	got := exec(t, "", "merge", "--on-conflict=first", a, b)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "description: ok") {
		t.Errorf("the first definition should have won:\n%s", got.stdout)
	}
}

func TestDryRunWritesNothing(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	out := filepath.Join(dir, "out.yaml")

	got := exec(t, "", "merge", "--dry-run", "-o", out, a)
	if got.code != ExitOK {
		t.Fatalf("code = %d", got.code)
	}
	if _, err := os.Stat(out); !os.IsNotExist(err) {
		t.Error("--dry-run created the output file")
	}
	if !strings.Contains(got.stderr, "dry run") {
		t.Errorf("stderr:\n%s", got.stderr)
	}
}

func TestQuietSuppressesDiagnostics(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", specB)

	got := exec(t, "", "merge", "-q", a, b)
	if got.code != ExitOK {
		t.Fatalf("code = %d", got.code)
	}
	if got.stderr != "" {
		t.Errorf("-q should silence stderr, got:\n%s", got.stderr)
	}
}

func TestJSONLogFormat(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", specB)

	got := exec(t, "", "merge", "--log-format=json", "-o", filepath.Join(dir, "o.yaml"), a, b)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(got.stderr), "\n") {
		if !strings.HasPrefix(line, "{") {
			continue
		}
		var d map[string]any
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			t.Fatalf("diagnostic is not JSON: %v\n%s", err, line)
		}
		if d["severity"] == nil || d["message"] == nil {
			t.Errorf("diagnostic is missing fields: %v", d)
		}
		found = true
	}
	if !found {
		t.Errorf("no JSON diagnostics emitted:\n%s", got.stderr)
	}
}

func TestValidate(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)

	got := exec(t, "", "validate", a)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	if !strings.Contains(got.stdout, "ok:") {
		t.Errorf("stdout:\n%s", got.stdout)
	}
}

func TestValidateReportsDanglingRef(t *testing.T) {
	dir := t.TempDir()
	bad := write(t, dir, "bad.yaml", `openapi: 3.0.3
info: {title: A, version: "1.0"}
paths:
  /x:
    get:
      responses:
        "200":
          description: ok
          content:
            application/json:
              schema: {$ref: "#/components/schemas/Nope"}
`)
	got := exec(t, "", "validate", bad)
	if got.code != ExitFailure {
		t.Fatalf("code = %d, want %d", got.code, ExitFailure)
	}
	if !strings.Contains(got.stderr, "does not resolve") {
		t.Errorf("stderr:\n%s", got.stderr)
	}
}

func TestUsageErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{"no arguments", nil},
		{"no inputs", []string{"merge"}},
		{"unknown command", []string{"frobnicate"}},
		{"unknown flag", []string{"merge", "--nope", "a.yaml"}},
		{"bad policy", []string{"merge", "--on-conflict=maybe", "a.yaml"}},
		{"bad format", []string{"merge", "--format=xml", "a.yaml"}},
		{"bad log format", []string{"merge", "--log-format=xml", "a.yaml"}},
		{"bad section policy", []string{"merge", "--on-conflict-section=schemas", "a.yaml"}},
		{"validate without inputs", []string{"validate"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := exec(t, "", tc.args...); got.code != ExitUsage {
				t.Errorf("code = %d, want %d\nstderr: %s", got.code, ExitUsage, got.stderr)
			}
		})
	}
}

func TestMissingFileExitsOne(t *testing.T) {
	got := exec(t, "", "merge", filepath.Join(t.TempDir(), "absent.yaml"))
	if got.code != ExitFailure {
		t.Fatalf("code = %d, want %d", got.code, ExitFailure)
	}
}

func TestVersionAndHelp(t *testing.T) {
	v := exec(t, "", "version")
	if v.code != ExitOK || !strings.Contains(v.stdout, "swagger-merger test") {
		t.Errorf("version output: code=%d\n%s", v.code, v.stdout)
	}

	for _, args := range [][]string{{"help"}, {"help", "merge"}, {"help", "validate"}, {"--help"}} {
		got := exec(t, "", args...)
		if got.code != ExitOK {
			t.Errorf("%v: code = %d", args, got.code)
		}
		if got.stdout == "" {
			t.Errorf("%v: no help text", args)
		}
	}
}

func TestParseSectionPolicies(t *testing.T) {
	got, err := parseSectionPolicies([]string{"schemas=first", "tags=last,paths=error"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("parsed %d policies, want 3: %v", len(got), got)
	}
	if _, err := parseSectionPolicies([]string{"schemas"}); err == nil {
		t.Error("expected an error for a pair without =")
	}
	if _, err := parseSectionPolicies([]string{"schemas=nonsense"}); err == nil {
		t.Error("expected an error for an unknown policy")
	}
}

func TestResolveFormat(t *testing.T) {
	tests := []struct {
		explicit, out, want string
		wantErr             bool
	}{
		{"", "-", "yaml", false},
		{"", "out.yaml", "yaml", false},
		{"", "out.JSON", "json", false},
		{"json", "out.yaml", "json", false},
		{"yml", "", "yaml", false},
		{"xml", "", "", true},
	}
	for _, tc := range tests {
		got, err := resolveFormat(tc.explicit, tc.out)
		if (err != nil) != tc.wantErr {
			t.Errorf("resolveFormat(%q, %q) err = %v", tc.explicit, tc.out, err)
		}
		if got != tc.want {
			t.Errorf("resolveFormat(%q, %q) = %q, want %q", tc.explicit, tc.out, got, tc.want)
		}
	}
}

// Not every output target can be replaced by a rename. Devices and FIFOs are
// ordinary destinations that the atomic path used to reject outright.
func TestWriteToNonRegularFile(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)

	got := exec(t, "", "merge", "-q", "-o", os.DevNull, a)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
}

// A symlinked output must stay a symlink rather than be replaced by a file.
func TestWriteThroughSymlink(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	target := write(t, dir, "real.yaml", "# placeholder\n")
	link := filepath.Join(dir, "link.yaml")
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	if got := exec(t, "", "merge", "-q", "-o", link, a); got.code != ExitOK {
		t.Fatalf("code = %d", got.code)
	}
	info, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("the symlink was replaced by a regular file")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "/users") {
		t.Errorf("the link target was not written:\n%s", data)
	}
}

// The output/input notice is a coded diagnostic, so --log-format json must not
// leave a bare text line in an otherwise machine-readable stream.
func TestOutputIsInputRespectsLogFormat(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", specB)

	got := exec(t, "", "merge", "--log-format=json", "-o", a, a, b)
	if got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	var found bool
	for _, line := range strings.Split(strings.TrimSpace(got.stderr), "\n") {
		if line == "" || strings.HasPrefix(line, "merged ") {
			continue
		}
		if !strings.HasPrefix(line, "{") {
			t.Errorf("non-JSON diagnostic line under --log-format=json: %q", line)
			continue
		}
		var d map[string]any
		if err := json.Unmarshal([]byte(line), &d); err != nil {
			t.Fatalf("bad JSON: %v\n%s", err, line)
		}
		if d["code"] == "output-is-input" {
			found = true
		}
	}
	if !found {
		t.Errorf("the output-is-input warning was not emitted as JSON:\n%s", got.stderr)
	}
}

// --strict must not fail a normal multi-document merge from the command line
// either; the README documents exactly this invocation for CI.
func TestStrictMergeOfOrdinaryDocuments(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.yaml", specA)
	b := write(t, dir, "b.yaml", specB)

	if got := exec(t, "", "merge", "--strict", "-q", "-o", filepath.Join(dir, "o.yaml"), a, b); got.code != ExitOK {
		t.Fatalf("code = %d, stderr:\n%s", got.code, got.stderr)
	}
	if got := exec(t, "", "validate", "--strict", a, b); got.code != ExitOK {
		t.Fatalf("validate --strict code = %d, stderr:\n%s", got.code, got.stderr)
	}
}
