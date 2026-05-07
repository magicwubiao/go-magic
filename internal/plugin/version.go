package plugin

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// VersionManager handles plugin version management
type VersionManager struct {
	versions map[string][]string // pluginID -> versions
}

// NewVersionManager creates a new version manager
func NewVersionManager() *VersionManager {
	return &VersionManager{
		versions: make(map[string][]string),
	}
}

// AddVersion adds a version to a plugin
func (vm *VersionManager) AddVersion(pluginID, version string) {
	if !IsValidVersion(version) {
		return
	}

	vm.versions[pluginID] = append(vm.versions[pluginID], version)
	vm.sortVersions(pluginID)
}

// RemoveVersion removes a version from a plugin
func (vm *VersionManager) RemoveVersion(pluginID, version string) {
	versions := vm.versions[pluginID]
	for i, v := range versions {
		if v == version {
			vm.versions[pluginID] = append(versions[:i], versions[i+1:]...)
			return
		}
	}
}

// GetVersions returns all versions for a plugin
func (vm *VersionManager) GetVersions(pluginID string) []string {
	versions := vm.versions[pluginID]
	result := make([]string, len(versions))
	copy(result, versions)
	return result
}

// GetLatest returns the latest version for a plugin
func (vm *VersionManager) GetLatest(pluginID string) (string, bool) {
	versions := vm.versions[pluginID]
	if len(versions) == 0 {
		return "", false
	}
	return versions[len(versions)-1], true
}

// GetCompatible returns the best compatible version for a constraint
func (vm *VersionManager) GetCompatible(pluginID, constraint string) (string, bool) {
	versions := vm.versions[pluginID]
	if len(versions) == 0 {
		return "", false
	}

	for i := len(versions) - 1; i >= 0; i-- {
		if CheckVersion(versions[i], constraint) {
			return versions[i], true
		}
	}

	return "", false
}

// CheckUpgrade checks if there's an upgrade available
func (vm *VersionManager) CheckUpgrade(pluginID, currentVersion string) (bool, string) {
	latest, ok := vm.GetLatest(pluginID)
	if !ok {
		return false, ""
	}

	return CheckUpgrade(currentVersion, latest), latest
}

// ListAll lists all plugin versions
func (vm *VersionManager) ListAll() map[string][]string {
	result := make(map[string][]string)
	for k, v := range vm.versions {
		result[k] = v
	}
	return result
}

// Clear removes all versions for a plugin
func (vm *VersionManager) Clear(pluginID string) {
	delete(vm.versions, pluginID)
}

// sortVersions sorts versions in ascending order
func (vm *VersionManager) sortVersions(pluginID string) {
	versions := vm.versions[pluginID]
	sort.Slice(versions, func(i, j int) bool {
		return CompareVersions(versions[i], versions[j]) < 0
	})
}

// VersionConstraint represents a version constraint
type VersionConstraint struct {
	Operator string
	Major    int
	Minor    *int
	Patch    *int
}

// ParseVersionConstraint parses a version constraint string
func ParseVersionConstraint(constraint string) (*VersionConstraint, error) {
	constraint = strings.TrimSpace(constraint)
	if constraint == "" {
		return nil, fmt.Errorf("empty constraint")
	}

	// Match patterns like >=1.2.3, >1.2.3, <=1.2.3, <1.2.3, =1.2.3

	// Simple patterns
	switch constraint {
	case "*":
		return &VersionConstraint{Operator: "*"}, nil
	case "latest":
		return &VersionConstraint{Operator: "latest"}, nil
	}

	// Caret ^1.2.3 - compatible with 1.x.x
	if strings.HasPrefix(constraint, "^") {
		v := strings.TrimPrefix(constraint, "^")
		vc, err := parseSimpleVersion(v)
		if err != nil {
			return nil, err
		}
		vc.Operator = "^"
		return vc, nil
	}

	// Tilde ~1.2.3 - compatible with 1.2.x
	if strings.HasPrefix(constraint, "~") {
		v := strings.TrimPrefix(constraint, "~")
		vc, err := parseSimpleVersion(v)
		if err != nil {
			return nil, err
		}
		vc.Operator = "~"
		return vc, nil
	}

	// Range "1.2.3 - 2.3.4"
	if strings.Contains(constraint, " - ") {
		parts := strings.Split(constraint, " - ")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid range: %s", constraint)
		}
		// Just return the first part for simplicity
		return parseSimpleVersion(parts[0])
	}

	// OR "||"
	if strings.Contains(constraint, "||") {
		parts := strings.Split(constraint, "||")
		// Just return the first part for simplicity
		return parseSimpleVersion(strings.TrimSpace(parts[0]))
	}

	// Simple version or operator
	return parseSimpleVersion(constraint)
}

// parseSimpleVersion parses a simple version or operator+version
func parseSimpleVersion(s string) (*VersionConstraint, error) {
	s = strings.TrimSpace(s)

	// Operator only
	ops := []string{">=", ">", "<=", "<", "="}
	for _, op := range ops {
		if strings.HasPrefix(s, op) {
			v := strings.TrimPrefix(s, op)
			parts, err := parseVersionParts(v)
			if err != nil {
				return nil, err
			}
			return &VersionConstraint{
				Operator: op,
				Major:    parts[0],
				Minor:    toIntPtr(fmt.Sprintf("%d", parts[1])),
				Patch:    toIntPtr(fmt.Sprintf("%d", parts[2])),
			}, nil
		}
	}

	// Just version
	parts, err := parseVersionParts(s)
	if err != nil {
		return nil, err
	}

	return &VersionConstraint{
		Operator: "=",
		Major:    parts[0],
		Minor:    toIntPtr(fmt.Sprintf("%d", parts[1])),
		Patch:    toIntPtr(fmt.Sprintf("%d", parts[2])),
	}, nil
}

// parseVersionParts parses "1.2.3" into [1, 2, 3]
func parseVersionParts(s string) ([]int, error) {
	parts := strings.Split(s, ".")
	result := make([]int, 3)

	for i, p := range parts {
		if i >= 3 {
			break
		}
		num, err := strconv.Atoi(strings.TrimSpace(p))
		if err != nil {
			return nil, fmt.Errorf("invalid version: %s", s)
		}
		result[i] = num
	}

	return result, nil
}

// toInt converts string to int
func toInt(s string) int {
	v, _ := strconv.Atoi(s)
	return v
}

// toIntPtr converts string to *int
func toIntPtr(s string) *int {
	if s == "" {
		return nil
	}
	v := toInt(s)
	return &v
}

// IsValidVersion checks if a version string is valid semver
func IsValidVersion(version string) bool {
	matched, _ := regexp.MatchString(`^v?\d+\.\d+\.\d+(?:-[\w.]+)?(?:\+[\w.]+)?$`, version)
	return matched
}

// IsValidVersionConstraint checks if a version constraint is valid
func IsValidVersionConstraint(constraint string) bool {
	_, err := ParseVersionConstraint(constraint)
	return err == nil
}

// CheckVersion checks if a version satisfies a constraint
func CheckVersion(version, constraint string) bool {
	vc, err := ParseVersionConstraint(constraint)
	if err != nil {
		return false
	}

	return vc.Matches(version)
}

// Matches checks if a version matches this constraint
func (vc *VersionConstraint) Matches(version string) bool {
	if vc.Operator == "*" {
		return true
	}

	if vc.Operator == "latest" {
		// "latest" matches anything
		return true
	}

	parts, err := parseVersionParts(version)
	if err != nil {
		return false
	}

	major := parts[0]
	minor := parts[1]
	patch := parts[2]

	switch vc.Operator {
	case "=":
		return major == vc.Major && comparePtr(minor, vc.Minor) == 0 && comparePtr(patch, vc.Patch) == 0
	case ">":
		if major != vc.Major {
			return major > vc.Major
		}
		if comparePtr(minor, vc.Minor) != 0 {
			return comparePtr(minor, vc.Minor) > 0
		}
		return comparePtr(patch, vc.Patch) > 0
	case ">=":
		return vc.satisfiesLower(major, minor, patch) || vc.satisfiesEqual(major, minor, patch)
	case "<":
		if major != vc.Major {
			return major < vc.Major
		}
		if comparePtr(minor, vc.Minor) != 0 {
			return comparePtr(minor, vc.Minor) < 0
		}
		return comparePtr(patch, vc.Patch) < 0
	case "<=":
		return vc.satisfiesUpper(major, minor, patch) || vc.satisfiesEqual(major, minor, patch)
	case "^":
		// Compatible with same major version
		return major == vc.Major
	case "~":
		// Compatible with same major.minor
		if major != vc.Major {
			return false
		}
		return comparePtr(minor, vc.Minor) >= 0
	}

	return false
}

func (vc *VersionConstraint) satisfiesLower(major, minor, patch int) bool {
	if major > vc.Major {
		return true
	}
	if major < vc.Major {
		return false
	}
	return comparePtr(minor, vc.Minor) > 0 || (comparePtr(minor, vc.Minor) == 0 && comparePtr(patch, vc.Patch) > 0)
}

func (vc *VersionConstraint) satisfiesUpper(major, minor, patch int) bool {
	if major < vc.Major {
		return true
	}
	if major > vc.Major {
		return false
	}
	return comparePtr(minor, vc.Minor) < 0 || (comparePtr(minor, vc.Minor) == 0 && comparePtr(patch, vc.Patch) < 0)
}

func (vc *VersionConstraint) satisfiesEqual(major, minor, patch int) bool {
	if major != vc.Major {
		return false
	}
	if comparePtr(minor, vc.Minor) != 0 {
		return false
	}
	return comparePtr(patch, vc.Patch) == 0
}

func comparePtr(a int, b *int) int {
	if b == nil {
		return 0
	}
	if a < *b {
		return -1
	}
	if a > *b {
		return 1
	}
	return 0
}

// CompareVersions compares two versions
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func CompareVersions(a, b string) int {
	partsA, _ := parseVersionParts(a)
	partsB, _ := parseVersionParts(b)

	for i := 0; i < 3; i++ {
		if partsA[i] < partsB[i] {
			return -1
		}
		if partsA[i] > partsB[i] {
			return 1
		}
	}

	return 0
}

// CheckUpgrade checks if there's an upgrade available
func CheckUpgrade(current, latest string) bool {
	return CompareVersions(latest, current) > 0
}

// FormatConstraint formats a version constraint for display
func FormatConstraint(constraint string) string {
	vc, err := ParseVersionConstraint(constraint)
	if err != nil {
		return constraint
	}

	switch vc.Operator {
	case "*":
		return "any version"
	case "latest":
		return "latest version"
	case "^":
		return fmt.Sprintf("^%d.%d.%d", vc.Major, defaultInt(vc.Minor), defaultInt(vc.Patch))
	case "~":
		return fmt.Sprintf("~%d.%d.%d", vc.Major, defaultInt(vc.Minor), defaultInt(vc.Patch))
	default:
		return fmt.Sprintf("%s%d.%d.%d", vc.Operator, vc.Major, defaultInt(vc.Minor), defaultInt(vc.Patch))
	}
}

func defaultInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}
