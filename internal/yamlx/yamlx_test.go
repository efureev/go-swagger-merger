package yamlx

import (
	"errors"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

func parse(t *testing.T, src string) *yaml.Node {
	t.Helper()
	var n yaml.Node
	if err := yaml.Unmarshal([]byte(src), &n); err != nil {
		t.Fatalf("parse: %v", err)
	}
	return Unwrap(&n)
}

func TestMapSetPreservesOrder(t *testing.T) {
	n := parse(t, "a: 1\nb: 2\nc: 3\n")

	MapSet(n, "b", NewScalar("two")) // replace in place
	MapSet(n, "d", NewScalar("4"))   // append

	if got, want := strings.Join(MapKeys(n), ","), "a,b,c,d"; got != want {
		t.Errorf("keys = %q, want %q", got, want)
	}
	if v, _ := MapString(n, "b"); v != "two" {
		t.Errorf("b = %q, want %q", v, "two")
	}
}

func TestMapDelete(t *testing.T) {
	n := parse(t, "a: 1\nb: 2\nc: 3\n")
	if !MapDelete(n, "b") {
		t.Fatal("MapDelete reported b missing")
	}
	if MapDelete(n, "zz") {
		t.Error("MapDelete reported a missing key as present")
	}
	if got, want := strings.Join(MapKeys(n), ","), "a,c"; got != want {
		t.Errorf("keys = %q, want %q", got, want)
	}
}

// MapDelete must not corrupt a mapping it shares backing array with.
func TestMapDeleteDoesNotAliasSibling(t *testing.T) {
	n := parse(t, "a: 1\nb: 2\nc: 3\n")
	clone := Clone(n)
	MapDelete(n, "a")
	if got, want := strings.Join(MapKeys(clone), ","), "a,b,c"; got != want {
		t.Errorf("clone keys = %q, want %q", got, want)
	}
}

func TestEqual(t *testing.T) {
	tests := []struct {
		name string
		a, b string
		want bool
	}{
		{"identical", "a: 1\nb: 2\n", "a: 1\nb: 2\n", true},
		{"mapping order irrelevant", "a: 1\nb: 2\n", "b: 2\na: 1\n", true},
		{"comments irrelevant", "# note\na: 1\n", "a: 1 # other\n", true},
		{"quoting irrelevant", "a: \"x\"\n", "a: x\n", true},
		{"float spelling", "a: 1.0\n", "a: 1.00\n", true},
		{"int vs float differ", "a: 1\n", "a: 1.0\n", false},
		{"sequence order matters", "a: [1, 2]\n", "a: [2, 1]\n", false},
		{"value differs", "a: 1\n", "a: 2\n", false},
		{"extra key", "a: 1\n", "a: 1\nb: 2\n", false},
		{"nested", "a: {b: {c: [1, 2]}}\n", "a: {b: {c: [1, 2]}}\n", true},
		{"null forms", "a: ~\n", "a: null\n", true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Equal(parse(t, tc.a), parse(t, tc.b)); got != tc.want {
				t.Errorf("Equal = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestCanonicalIsOrderIndependent(t *testing.T) {
	a := Canonical(parse(t, "b: 2\na: {y: 1, x: 2}\n"))
	b := Canonical(parse(t, "a: {x: 2, y: 1}\nb: 2\n"))
	if a != b {
		t.Errorf("canonical forms differ:\n%s\n%s", a, b)
	}
	if c := Canonical(parse(t, "a: 1\n")); c == a {
		t.Error("different documents produced the same canonical form")
	}
}

func TestCloneIsIndependent(t *testing.T) {
	orig := parse(t, "a:\n  b: [1, 2]\n")
	cp := Clone(orig)

	inner, _ := MapValue(cp, "a")
	MapSet(inner, "b", NewScalar("mutated"))

	origInner, _ := MapValue(orig, "a")
	v, _ := MapValue(origInner, "b")
	if !IsSequence(v) {
		t.Fatalf("mutating the clone changed the original: %#v", v)
	}
}

func TestPointerAt(t *testing.T) {
	doc := parse(t, `
components:
  schemas:
    User:
      type: object
paths:
  /a/b:
    get: {x: 1}
list: [10, 20]
weird~key: yes
`)
	tests := []struct {
		pointer string
		want    bool
	}{
		{"/components/schemas/User", true},
		{"#/components/schemas/User", true},
		{"/paths/~1a~1b/get", true},
		{"/weird~0key", true},
		{"/list/1", true},
		{"/list/9", false},
		{"/list/notanindex", false},
		{"/components/schemas/Missing", false},
		{"", true},
		{"relative", false},
	}
	for _, tc := range tests {
		t.Run(tc.pointer, func(t *testing.T) {
			if _, ok := At(doc, tc.pointer); ok != tc.want {
				t.Errorf("At(%q) ok = %v, want %v", tc.pointer, ok, tc.want)
			}
		})
	}
}

func TestWalkVisitsEveryNodeWithPointer(t *testing.T) {
	doc := parse(t, "a:\n  b: [1]\n")
	var seen []string
	err := Walk(doc, func(p string, _ *yaml.Node) error {
		seen = append(seen, p)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"", "/a", "/a/b", "/a/b/0"}
	if strings.Join(seen, "|") != strings.Join(want, "|") {
		t.Errorf("pointers = %v, want %v", seen, want)
	}
}

func TestExpandAliases(t *testing.T) {
	doc := parse(t, `
base: &base
  kind: thing
  size: 1
copy: *base
`)
	if err := ExpandAliases(doc, DefaultAliasBudget); err != nil {
		t.Fatal(err)
	}
	cp, _ := MapValue(doc, "copy")
	if cp.Kind == yaml.AliasNode {
		t.Fatal("alias node survived expansion")
	}
	if v, _ := MapString(cp, "kind"); v != "thing" {
		t.Errorf("expanded copy = %v, want kind=thing", MapKeys(cp))
	}
	// The expansion must be a copy, not a shared pointer.
	MapSet(cp, "kind", NewScalar("mutated"))
	base, _ := MapValue(doc, "base")
	if v, _ := MapString(base, "kind"); v != "thing" {
		t.Error("expanded alias still aliases the anchor target")
	}
}

func TestExpandAliasesMergeKey(t *testing.T) {
	doc := parse(t, `
defaults: &d
  a: 1
  b: 2
target:
  <<: *d
  b: overridden
`)
	if err := ExpandAliases(doc, DefaultAliasBudget); err != nil {
		t.Fatal(err)
	}
	target, _ := MapValue(doc, "target")
	if _, _, ok := MapGet(target, "<<"); ok {
		t.Error("merge key survived expansion")
	}
	if v, _ := MapString(target, "a"); v != "1" {
		t.Errorf("a = %q, want 1 (merged in)", v)
	}
	if v, _ := MapString(target, "b"); v != "overridden" {
		t.Errorf("b = %q, want overridden (local key wins)", v)
	}
}

// A billion-laughs document must be rejected by the budget rather than
// exhausting memory.
func TestExpandAliasesBudget(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("a: &a [x, x, x, x, x, x, x, x, x]\n")
	for _, c := range "bcdefghij" {
		prev := string(c - 1)
		sb.WriteString(string(c) + ": &" + string(c) + " [")
		for i := 0; i < 9; i++ {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString("*" + prev)
		}
		sb.WriteString("]\n")
	}
	doc := parse(t, sb.String())

	err := ExpandAliases(doc, 10000)
	if !errors.Is(err, ErrAliasBudget) {
		t.Fatalf("err = %v, want ErrAliasBudget", err)
	}
}

func TestToJSONPreservesKeyOrder(t *testing.T) {
	doc := parse(t, "zeta: 1\nalpha: 2\nmid: {b: true, a: null}\n")
	got, err := ToJSON(doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"zeta":1,"alpha":2,"mid":{"b":true,"a":null}}` + "\n"
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestToJSONScalarTypes(t *testing.T) {
	doc := parse(t, `
i: 42
hex: 0x1f
f: 1.5
b: true
s: "7"
n: null
html: "a < b & c"
empty_map: {}
empty_seq: []
`)
	got, err := ToJSON(doc, 0)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"i":42,"hex":31,"f":1.5,"b":true,"s":"7","n":null,"html":"a < b & c","empty_map":{},"empty_seq":[]}` + "\n"
	if string(got) != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}

func TestSortMapping(t *testing.T) {
	n := parse(t, "c: 1\na: 2\nb: 3\n")
	SortMapping(n, func(a, b string) bool { return a < b })
	if got, want := strings.Join(MapKeys(n), ","), "a,b,c"; got != want {
		t.Errorf("keys = %q, want %q", got, want)
	}
}
