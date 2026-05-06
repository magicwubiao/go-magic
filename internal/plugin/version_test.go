package plugin

import (
	"testing"
)

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		a      string
		b      string
		expect int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.0", "2.0.0", -1},
		{"2.0.0", "1.0.0", 1},
		{"1.0.0", "1.1.0", -1},
		{"1.1.0", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"1.0.1", "1.0.0", 1},
		{"v1.0.0", "1.0.0", 0},
		{"1.0.0-alpha", "1.0.0", 0},
	}

	for _, tt := range tests {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			result := CompareVersions(tt.a, tt.b)
			if result != tt.expect {
				t.Errorf("CompareVersions(%s, %s) = %d, want %d", tt.a, tt.b, result, tt.expect)
			}
		})
	}
}

func TestIsValidVersion(t *testing.T) {
	tests := []struct {
		version string
		valid   bool
	}{
		{"1.0.0", true},
		{"v1.0.0", true},
		{"1.0.0-alpha", true},
		{"1.0.0-beta.1", true},
		{"1.0.0+build.123", true},
		{"1.0.0-alpha+build.123", true},
		{"invalid", false},
		{"1.0", false},
		{"1", false},
		{"", false},
	}

	for _, tt := range tests {
		t.Run(tt.version, func(t *testing.T) {
			result := IsValidVersion(tt.version)
			if result != tt.valid {
				t.Errorf("IsValidVersion(%s) = %v, want %v", tt.version, result, tt.valid)
			}
		})
	}
}

func TestParseVersionConstraint(t *testing.T) {
	tests := []struct {
		input      string
		operator   string
		major      int
		hasMinor   bool
		hasPatch   bool
		wantErr    bool
	}{
		{"*", "*", 0, false, false, false},
		{"latest", "latest", 0, false, false, false},
		{"1.0.0", "=", 1, true, true, false},
		{">=1.0.0", ">=", 1, true, true, false},
		{">1.0.0", ">", 1, true, true, false},
		{"<=1.0.0", "<=", 1, true, true, false},
		{"<1.0.0", "<", 1, true, true, false},
		{"^1.0.0", "^", 1, true, true, false},
		{"~1.2.0", "~", 1, true, true, false},
		{"1", "", 0, false, false, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			vc, err := ParseVersionConstraint(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseVersionConstraint(%s) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if tt.wantErr {
				return
			}

			if vc.Operator != tt.operator {
				t.Errorf("operator = %s, want %s", vc.Operator, tt.operator)
			}
			if vc.Major != tt.major {
				t.Errorf("major = %d, want %d", vc.Major, tt.major)
			}
			if (vc.Minor != nil) != tt.hasMinor {
				t.Errorf("hasMinor = %v, want %v", vc.Minor != nil, tt.hasMinor)
			}
			if (vc.Patch != nil) != tt.hasPatch {
				t.Errorf("hasPatch = %v, want %v", vc.Patch != nil, tt.hasPatch)
			}
		})
	}
}

func TestVersionConstraintMatches(t *testing.T) {
	tests := []struct {
		constraint string
		version    string
		matches    bool
	}{
		// Exact match
		{"1.0.0", "1.0.0", true},
		{"1.0.0", "1.0.1", false},
		{"=1.0.0", "1.0.0", true},

		// Greater than
		{">1.0.0", "1.0.1", true},
		{">1.0.0", "1.0.0", false},
		{">1.0.0", "0.9.9", false},

		// Greater than or equal
		{">=1.0.0", "1.0.0", true},
		{">=1.0.0", "1.0.1", true},
		{">=1.0.0", "0.9.9", false},

		// Less than
		{"<2.0.0", "1.9.9", true},
		{"<2.0.0", "2.0.0", false},
		{"<2.0.0", "2.0.1", false},

		// Less than or equal
		{"<=2.0.0", "2.0.0", true},
		{"<=2.0.0", "1.9.9", true},
		{"<=2.0.0", "2.0.1", false},

		// Caret (compatible with same major)
		{"^1.0.0", "1.9.9", true},
		{"^1.0.0", "2.0.0", false},
		{"^1.0.0", "1.0.0", true},
		{"^0.0.0", "0.0.1", false},

		// Tilde (compatible with same minor)
		{"~1.2.0", "1.2.9", true},
		{"~1.2.0", "1.3.0", false},
		{"~1.2.0", "1.2.0", true},

		// Wildcard
		{"*", "1.0.0", true},
		{"*", "999.999.999", true},

		// latest
		{"latest", "1.0.0", true},
	}

	for _, tt := range tests {
		t.Run(tt.constraint+"_"+tt.version, func(t *testing.T) {
			vc, err := ParseVersionConstraint(tt.constraint)
			if err != nil {
				t.Fatalf("failed to parse constraint: %v", err)
			}

			result := vc.Matches(tt.version)
			if result != tt.matches {
				t.Errorf("constraint %s matches %s = %v, want %v", tt.constraint, tt.version, result, tt.matches)
			}
		})
	}
}

func TestCheckVersion(t *testing.T) {
	tests := []struct {
		version   string
		constraint string
		matches   bool
	}{
		{"1.0.0", ">=1.0.0", true},
		{"1.0.0", "^1.0.0", true},
		{"2.0.0", "^1.0.0", false},
		{"1.9.9", "^1.0.0", true},
		{"invalid", "^1.0.0", false},
	}

	for _, tt := range tests {
		t.Run(tt.constraint+"_"+tt.version, func(t *testing.T) {
			result := CheckVersion(tt.version, tt.constraint)
			if result != tt.matches {
				t.Errorf("CheckVersion(%s, %s) = %v, want %v", tt.version, tt.constraint, result, tt.matches)
			}
		})
	}
}

func TestCheckUpgrade(t *testing.T) {
	if !CheckUpgrade("1.0.0", "1.0.1") {
		t.Error("expected upgrade available")
	}
	if !CheckUpgrade("1.0.0", "2.0.0") {
		t.Error("expected upgrade available")
	}
	if CheckUpgrade("1.0.0", "1.0.0") {
		t.Error("expected no upgrade")
	}
	if CheckUpgrade("1.0.1", "1.0.0") {
		t.Error("expected no upgrade")
	}
}

func TestNewVersionManager(t *testing.T) {
	vm := NewVersionManager()
	if vm == nil {
		t.Fatal("expected non-nil version manager")
	}
	if vm.versions == nil {
		t.Error("expected versions map to be initialized")
	}
}

func TestVersionManagerAddVersion(t *testing.T) {
	vm := NewVersionManager()

	vm.AddVersion("plugin1", "1.0.0")
	vm.AddVersion("plugin1", "2.0.0")
	vm.AddVersion("plugin1", "1.5.0")

	versions := vm.GetVersions("plugin1")
	if len(versions) != 3 {
		t.Errorf("expected 3 versions, got %d", len(versions))
	}

	// Should be sorted
	if versions[0] != "1.0.0" {
		t.Errorf("expected first version 1.0.0, got %s", versions[0])
	}
	if versions[2] != "2.0.0" {
		t.Errorf("expected last version 2.0.0, got %s", versions[2])
	}
}

func TestVersionManagerGetLatest(t *testing.T) {
	vm := NewVersionManager()

	vm.AddVersion("plugin1", "1.0.0")
	vm.AddVersion("plugin1", "2.0.0")
	vm.AddVersion("plugin1", "1.5.0")

	latest, ok := vm.GetLatest("plugin1")
	if !ok {
		t.Error("expected to get latest version")
	}
	if latest != "2.0.0" {
		t.Errorf("expected latest=2.0.0, got %s", latest)
	}

	// Non-existent plugin
	_, ok = vm.GetLatest("nonexistent")
	if ok {
		t.Error("expected not found for nonexistent plugin")
	}
}

func TestVersionManagerGetCompatible(t *testing.T) {
	vm := NewVersionManager()

	vm.AddVersion("plugin1", "1.0.0")
	vm.AddVersion("plugin1", "1.1.0")
	vm.AddVersion("plugin1", "2.0.0")

	// Compatible with ^1.0.0
	compat, ok := vm.GetCompatible("plugin1", "^1.0.0")
	if !ok {
		t.Error("expected compatible version")
	}
	if compat != "1.1.0" {
		t.Errorf("expected compat=1.1.0, got %s", compat)
	}

	// Compatible with ^2.0.0
	compat, ok = vm.GetCompatible("plugin1", "^2.0.0")
	if !ok {
		t.Error("expected compatible version")
	}
	if compat != "2.0.0" {
		t.Errorf("expected compat=2.0.0, got %s", compat)
	}
}

func TestVersionManagerCheckUpgrade(t *testing.T) {
	vm := NewVersionManager()

	vm.AddVersion("plugin1", "1.0.0")
	vm.AddVersion("plugin1", "1.1.0")
	vm.AddVersion("plugin1", "2.0.0")

	hasUpgrade, latest := vm.CheckUpgrade("plugin1", "1.0.0")
	if !hasUpgrade {
		t.Error("expected upgrade")
	}
	if latest != "2.0.0" {
		t.Errorf("expected latest=2.0.0, got %s", latest)
	}

	hasUpgrade, latest = vm.CheckUpgrade("plugin1", "2.0.0")
	if hasUpgrade {
		t.Error("expected no upgrade")
	}
}

func TestVersionManagerRemoveVersion(t *testing.T) {
	vm := NewVersionManager()

	vm.AddVersion("plugin1", "1.0.0")
	vm.AddVersion("plugin1", "2.0.0")

	vm.RemoveVersion("plugin1", "1.0.0")

	versions := vm.GetVersions("plugin1")
	if len(versions) != 1 {
		t.Errorf("expected 1 version, got %d", len(versions))
	}
	if versions[0] != "2.0.0" {
		t.Errorf("expected remaining version 2.0.0, got %s", versions[0])
	}
}

func TestFormatConstraint(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"*", "any version"},
		{"latest", "latest version"},
		{"^1.0.0", "^1.0.0"},
		{"~1.2.0", "~1.2.0"},
		{">=1.0.0", ">=1.0.0"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := FormatConstraint(tt.input)
			if result != tt.expected {
				t.Errorf("FormatConstraint(%s) = %s, want %s", tt.input, result, tt.expected)
			}
		})
	}
}
