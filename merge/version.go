package merge

import (
	"fmt"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/efureev/go-swagger-merger/v2/internal/yamlx"
)

// Family distinguishes the two incompatible spec lineages.
type Family uint8

// Spec families.
const (
	FamilyUnknown Family = iota
	FamilySwagger2
	FamilyOpenAPI3
)

func (f Family) String() string {
	switch f {
	case FamilySwagger2:
		return "swagger"
	case FamilyOpenAPI3:
		return "openapi"
	default:
		return "unknown"
	}
}

// RootKey is the document key that declares this family's version.
func (f Family) RootKey() string {
	if f == FamilySwagger2 {
		return "swagger"
	}
	return "openapi"
}

// SpecVersion is the declared version of an input document.
type SpecVersion struct {
	Family Family
	Raw    string
	Major  int
	Minor  int
	Patch  int
}

func (v SpecVersion) String() string {
	if v.Raw == "" {
		return "unknown"
	}
	return v.Family.RootKey() + " " + v.Raw
}

// IsZero reports whether no version was detected.
func (v SpecVersion) IsZero() bool { return v.Family == FamilyUnknown }

// DetectVersion reads the swagger/openapi key from a document root.
func DetectVersion(root *yaml.Node) (SpecVersion, error) {
	root = yamlx.Unwrap(root)
	if raw, ok := yamlx.MapString(root, "openapi"); ok {
		return parseVersion(FamilyOpenAPI3, raw)
	}
	if raw, ok := yamlx.MapString(root, "swagger"); ok {
		return parseVersion(FamilySwagger2, raw)
	}
	return SpecVersion{}, fmt.Errorf("%w: no \"openapi\" or \"swagger\" key", ErrInvalidDocument)
}

func parseVersion(f Family, raw string) (SpecVersion, error) {
	v := SpecVersion{Family: f, Raw: strings.TrimSpace(raw)}
	parts := strings.SplitN(v.Raw, ".", 3)
	nums := make([]int, 3)
	for i := range parts {
		// Trim any pre-release suffix, e.g. "3.1.0-rc1".
		digits := parts[i]
		if cut := strings.IndexAny(digits, "-+"); cut >= 0 {
			digits = digits[:cut]
		}
		n, err := strconv.Atoi(digits)
		if err != nil {
			return v, fmt.Errorf("%w: malformed version %q", ErrInvalidDocument, raw)
		}
		nums[i] = n
	}
	v.Major, v.Minor, v.Patch = nums[0], nums[1], nums[2]

	switch {
	case f == FamilySwagger2 && v.Major != 2:
		return v, fmt.Errorf("%w: swagger key declares version %q", ErrInvalidDocument, raw)
	case f == FamilyOpenAPI3 && v.Major < 3:
		return v, fmt.Errorf("%w: openapi key declares version %q", ErrInvalidDocument, raw)
	}
	return v, nil
}

// CompatibleWith reports whether two documents may be merged.
//
// Swagger 2.0 and OpenAPI 3.x have different document shapes, so mixing them
// is always refused. Mixing 3.0.x with 3.1.x is refused by default because
// 3.1 changed schema semantics (nullable, exclusiveMinimum) and added
// webhooks; allowSkew lifts that.
func (v SpecVersion) CompatibleWith(o SpecVersion, allowSkew bool) bool {
	if v.IsZero() || o.IsZero() {
		return true
	}
	if v.Family != o.Family || v.Major != o.Major {
		return false
	}
	return allowSkew || v.Minor == o.Minor
}

// Precedes reports whether v is older than o within the same major.minor.
func (v SpecVersion) Precedes(o SpecVersion) bool {
	if v.Major != o.Major {
		return v.Major < o.Major
	}
	if v.Minor != o.Minor {
		return v.Minor < o.Minor
	}
	return v.Patch < o.Patch
}
