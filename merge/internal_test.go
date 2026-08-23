package merge

import "testing"

func TestNormalizePathTemplate(t *testing.T) {
	tests := map[string]string{
		"/users":                   "/users",
		"/users/{id}":              "/users/{}",
		"/users/{userId}":          "/users/{}",
		"/users/{id}/pets/{petId}": "/users/{}/pets/{}",
		"/a/{}/b":                  "/a/{}/b",
		"/broken/{unclosed":        "/broken/{}",
		// A stray closing brace is not a template, so the path is left alone.
		"/weird/}stray": "/weird/}stray",
	}
	for in, want := range tests {
		if got := normalizePathTemplate(in); got != want {
			t.Errorf("normalizePathTemplate(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidComponentName(t *testing.T) {
	valid := []string{"User", "user_1", "a.b-c", "A"}
	invalid := []string{"", "with space", "a/b", "héllo", "a#b"}

	for _, s := range valid {
		if !validComponentName(s) {
			t.Errorf("%q should be a valid component name", s)
		}
	}
	for _, s := range invalid {
		if validComponentName(s) {
			t.Errorf("%q should not be a valid component name", s)
		}
	}
}

func TestClassifyRef(t *testing.T) {
	tests := map[string]RefKind{
		"#/components/schemas/User":           RefLocal,
		"#":                                   RefLocal,
		"./common.yaml#/components/schemas/E": RefFile,
		"common.yaml":                         RefFile,
		"../shared/defs.yaml#/definitions/X":  RefFile,
		"https://example.com/spec.yaml#/a":    RefRemote,
		"http://example.com/spec.yaml":        RefRemote,
		"//cdn.example.com/spec.yaml":         RefRemote,
	}
	for in, want := range tests {
		if got := classifyRef(in); got != want {
			t.Errorf("classifyRef(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseVersion(t *testing.T) {
	tests := []struct {
		family  Family
		raw     string
		wantErr bool
		major   int
		minor   int
		patch   int
	}{
		{FamilyOpenAPI3, "3.0.3", false, 3, 0, 3},
		{FamilyOpenAPI3, "3.1.0", false, 3, 1, 0},
		{FamilyOpenAPI3, "3.1.0-rc1", false, 3, 1, 0},
		{FamilyOpenAPI3, "3.0", false, 3, 0, 0},
		{FamilySwagger2, "2.0", false, 2, 0, 0},
		{FamilySwagger2, "3.0", true, 0, 0, 0},
		{FamilyOpenAPI3, "2.0", true, 0, 0, 0},
		{FamilyOpenAPI3, "banana", true, 0, 0, 0},
		{FamilyOpenAPI3, "", true, 0, 0, 0},
	}
	for _, tc := range tests {
		v, err := parseVersion(tc.family, tc.raw)
		if (err != nil) != tc.wantErr {
			t.Errorf("parseVersion(%v, %q) err = %v, wantErr %v", tc.family, tc.raw, err, tc.wantErr)
			continue
		}
		if tc.wantErr {
			continue
		}
		if v.Major != tc.major || v.Minor != tc.minor || v.Patch != tc.patch {
			t.Errorf("parseVersion(%q) = %d.%d.%d, want %d.%d.%d",
				tc.raw, v.Major, v.Minor, v.Patch, tc.major, tc.minor, tc.patch)
		}
	}
}
