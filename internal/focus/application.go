package focus

import (
	"strings"
)

const (
	UnassignedApplication = "(Unassigned)"
	aliasSeparator        = "|"
)

// NormalizeApplicationName canonicalizes application labels for grouping:
// trim, uppercase, spaces and hyphens become underscores.
func NormalizeApplicationName(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" {
		return "(UNASSIGNED)"
	}
	s = strings.ToUpper(s)
	s = strings.ReplaceAll(s, "-", "_")
	s = strings.ReplaceAll(s, " ", "_")
	return s
}

// CanonicalApplicationName collapses plural variants (e.g. NETWORKING_SERVICES → NETWORKING_SERVICE)
// by stripping a trailing S from the last underscore-separated segment.
func CanonicalApplicationName(norm string) string {
	norm = strings.TrimSpace(norm)
	if norm == "" || norm == "(UNASSIGNED)" {
		return "(UNASSIGNED)"
	}
	idx := strings.LastIndex(norm, "_")
	var prefix, last string
	if idx < 0 {
		last = norm
	} else {
		prefix = norm[:idx+1]
		last = norm[idx+1:]
	}
	if len(last) > 1 && strings.HasSuffix(last, "S") {
		return prefix + strings.TrimSuffix(last, "S")
	}
	return norm
}

// ResolveApplicationName returns the canonical dim_application key for a raw label.
func ResolveApplicationName(raw string) string {
	return CanonicalApplicationName(NormalizeApplicationName(raw))
}

// MergeAliasValues appends a raw alias to a pipe-separated alias list (case-insensitive dedup).
func MergeAliasValues(existing, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return strings.TrimSpace(existing)
	}
	existing = strings.TrimSpace(existing)
	if existing == "" {
		return raw
	}
	// tolerate legacy comma-separated values
	existing = strings.ReplaceAll(existing, ",", aliasSeparator)
	for _, part := range strings.Split(existing, aliasSeparator) {
		if strings.EqualFold(strings.TrimSpace(part), raw) {
			return existing
		}
	}
	return existing + aliasSeparator + raw
}
