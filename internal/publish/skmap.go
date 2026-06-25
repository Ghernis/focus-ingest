package publish

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/ghernis/focus_dt/internal/focus"
)

// skMaps holds local_sk -> server_sk remaps per dimension table.
type skMaps map[string]map[int64]int64

func (m skMaps) remap(dim string, sk interface{}) interface{} {
	if sk == nil {
		return nil
	}
	v, ok := int64From(sk)
	if !ok {
		return sk
	}
	if v == 0 {
		return sk
	}
	if dm := m[dim]; dm != nil {
		if nv, ok := dm[v]; ok {
			return nv
		}
	}
	return sk
}

func int64From(v interface{}) (int64, bool) {
	switch t := v.(type) {
	case int64:
		return t, true
	case int:
		return int64(t), true
	case float64:
		return int64(t), true
	case string:
		if t == "" {
			return 0, true
		}
		n, err := strconv.ParseInt(t, 10, 64)
		return n, err == nil
	default:
		n, err := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		return n, err == nil
	}
}

func int64FromRemap(v interface{}) int64 {
	if v == nil {
		return -1
	}
	n, ok := int64From(v)
	if !ok {
		return -1
	}
	return n
}

func grainKey(vals []interface{}, cols []int) string {
	return grainKeyWithNorm(vals, cols, nil)
}

type grainNorm byte

const (
	grainNormDefault grainNorm = iota
	grainNormDate
	grainNormFold // trim + uppercase for SQL Server CI unique keys
	grainNormInt
	grainNormTagKey   // VARCHAR(256)
	grainNormTagValue // NVARCHAR(512)
)

func grainKeyWithNorm(vals []interface{}, cols []int, norms []grainNorm) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		norm := grainNormDefault
		if i < len(norms) {
			norm = norms[i]
		}
		parts[i] = normalizeGrainPart(vals, c, norm)
	}
	return strings.Join(parts, "\x1f")
}

func normalizeGrainPart(vals []interface{}, col int, norm grainNorm) string {
	switch norm {
	case grainNormDate:
		s := normalizeDateKey(vals[col])
		vals[col] = s
		return s
	case grainNormFold:
		raw := strings.TrimSpace(fmt.Sprint(vals[col]))
		vals[col] = raw
		return strings.ToUpper(raw)
	case grainNormTagKey:
		raw := truncateRunes(strings.TrimSpace(fmt.Sprint(vals[col])), 256)
		vals[col] = raw
		return strings.ToUpper(raw)
	case grainNormTagValue:
		raw := truncateRunes(strings.TrimSpace(fmt.Sprint(vals[col])), 512)
		vals[col] = raw
		return strings.ToUpper(raw)
	case grainNormInt:
		n, ok := int64From(vals[col])
		if !ok {
			return ""
		}
		vals[col] = n
		return strconv.FormatInt(n, 10)
	default:
		if n, ok := int64From(vals[col]); ok {
			vals[col] = n
			return strconv.FormatInt(n, 10)
		}
		s := strings.TrimSpace(fmt.Sprint(vals[col]))
		if s == "<nil>" {
			s = ""
		}
		vals[col] = s
		return s
	}
}

func normalizeDateKey(v interface{}) string {
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "<nil>" {
		return ""
	}
	return focus.DateOnly(s)
}

func truncateRunes(s string, max int) string {
	if max <= 0 {
		return s
	}
	r := []rune(s)
	if len(r) > max {
		return string(r[:max])
	}
	return s
}

func sumFloat64(a, b interface{}) float64 {
	af, _ := strconv.ParseFloat(fmt.Sprint(a), 64)
	bf, _ := strconv.ParseFloat(fmt.Sprint(b), 64)
	return af + bf
}

func sumIntVals(a, b interface{}) int64 {
	ai, _ := int64From(a)
	bi, _ := int64From(b)
	return ai + bi
}

func mergeRows(existing, incoming []interface{}, decCols, intCols []int) {
	for _, c := range decCols {
		existing[c] = sumFloat64(existing[c], incoming[c])
	}
	for _, c := range intCols {
		existing[c] = sumIntVals(existing[c], incoming[c])
	}
}

type aggColKind byte

const (
	aggColString aggColKind = iota
	aggColInt
	aggColIntNull
	aggColDecimal
	aggColDate
	aggColDateTime
)

func coerceSQLDate(v interface{}) (time.Time, error) {
	if v == nil {
		return time.Time{}, fmt.Errorf("nil date")
	}
	switch t := v.(type) {
	case time.Time:
		return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC), nil
	}
	s := normalizeDateKey(v)
	if s == "" {
		return time.Time{}, fmt.Errorf("empty date from %#v", v)
	}
	parsed, err := time.Parse("2006-01-02", s)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse date %q: %w", s, err)
	}
	return parsed, nil
}

func coerceSQLDateTime(v interface{}) (time.Time, error) {
	if v == nil {
		return time.Time{}, fmt.Errorf("nil datetime")
	}
	switch t := v.(type) {
	case time.Time:
		return t.UTC(), nil
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return time.Time{}, fmt.Errorf("empty datetime")
	}
	lower := strings.ToLower(s)
	if lower == "datetime('now')" || lower == `datetime("now")` || lower == "sysutcdatetime()" {
		return time.Now().UTC(), nil
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, s); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("parse datetime %q", s)
}

func coerceAggVals(vals []interface{}, kinds []aggColKind) error {
	for i, k := range kinds {
		if i >= len(vals) {
			return nil
		}
		switch k {
		case aggColInt:
			n, ok := int64From(vals[i])
			if ok {
				vals[i] = n
			}
		case aggColIntNull:
			if vals[i] == nil {
				continue
			}
			s := strings.TrimSpace(fmt.Sprint(vals[i]))
			if s == "" || s == "<nil>" {
				vals[i] = nil
				continue
			}
			n, ok := int64From(vals[i])
			if ok {
				vals[i] = n
			}
		case aggColDecimal:
			f, err := strconv.ParseFloat(fmt.Sprint(vals[i]), 64)
			if err != nil {
				vals[i] = 0.0
			} else if math.IsNaN(f) || math.IsInf(f, 0) {
				vals[i] = 0.0
			} else {
				vals[i] = f
			}
		case aggColDate:
			dt, err := coerceSQLDate(vals[i])
			if err != nil {
				return fmt.Errorf("date column index %d: %w", i, err)
			}
			vals[i] = dt
		case aggColDateTime:
			dt, err := coerceSQLDateTime(vals[i])
			if err != nil {
				return fmt.Errorf("datetime column index %d: %w", i, err)
			}
			vals[i] = dt
		}
	}
	return nil
}

func formatAggRow(vals []interface{}, kinds []aggColKind) string {
	parts := make([]string, 0, len(vals))
	for i, v := range vals {
		label := fmt.Sprintf("c%d", i)
		if i < len(kinds) {
			switch kinds[i] {
			case aggColDate:
				label += ":date"
			case aggColDateTime:
				label += ":dt"
			case aggColDecimal:
				label += ":dec"
			case aggColInt, aggColIntNull:
				label += ":int"
			default:
				label += ":str"
			}
		}
		parts = append(parts, fmt.Sprintf("%s=%#v", label, v))
	}
	return strings.Join(parts, ", ")
}

