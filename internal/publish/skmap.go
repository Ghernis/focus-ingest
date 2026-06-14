package publish

import (
	"fmt"
	"strconv"
	"strings"
)

// skMaps holds local_sk -> server_sk remaps per dimension table.
type skMaps map[string]map[int64]int64

func (m skMaps) remap(dim string, sk interface{}) interface{} {
	if sk == nil {
		return nil
	}
	var v int64
	switch t := sk.(type) {
	case int64:
		v = t
	case int:
		v = int64(t)
	case float64:
		v = int64(t)
	default:
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

func int64FromRemap(v interface{}) int64 {
	if v == nil {
		return -1
	}
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	default:
		n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		return n
	}
}

func grainKey(vals []interface{}, cols []int) string {
	parts := make([]string, len(cols))
	for i, c := range cols {
		parts[i] = fmt.Sprint(vals[c])
	}
	return strings.Join(parts, "\x1f")
}

func sumDecimalStrings(a, b interface{}) string {
	af, _ := strconv.ParseFloat(fmt.Sprint(a), 64)
	bf, _ := strconv.ParseFloat(fmt.Sprint(b), 64)
	return fmt.Sprintf("%g", af+bf)
}

func sumIntVals(a, b interface{}) int64 {
	ai, _ := strconv.ParseInt(fmt.Sprint(a), 10, 64)
	bi, _ := strconv.ParseInt(fmt.Sprint(b), 10, 64)
	return ai + bi
}

func mergeRows(existing, incoming []interface{}, sumCols []int, sumInts bool) {
	for _, c := range sumCols {
		if sumInts {
			existing[c] = sumIntVals(existing[c], incoming[c])
		} else {
			existing[c] = sumDecimalStrings(existing[c], incoming[c])
		}
	}
}
