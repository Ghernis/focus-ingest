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
		return CanonicalAliasList(existing)
	}
	return MergeAliasLists(existing, raw)
}

func splitAliasList(s string) []string {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, ",", aliasSeparator)
	var out []string
	for _, part := range strings.Split(s, aliasSeparator) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// CanonicalAliasList normalizes a pipe/comma-separated alias list (dedup, pipe join).
func CanonicalAliasList(s string) string {
	return MergeAliasLists(s, "")
}

// MergeAliasLists unions two alias lists (case-insensitive dedup; first-seen casing wins).
func MergeAliasLists(a, b string) string {
	var merged []string
	seen := map[string]struct{}{}
	for _, part := range append(splitAliasList(a), splitAliasList(b)...) {
		key := strings.ToLower(part)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		merged = append(merged, part)
	}
	return strings.Join(merged, aliasSeparator)
}
