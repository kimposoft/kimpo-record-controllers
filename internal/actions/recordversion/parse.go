// Package recordversion owns the strict conversion from query-result values to
// optimistic-lock version tokens used by the official update/delete actions.
package recordversion

import (
	"encoding/json"
	"math"
	"strconv"
)

// Parse accepts only positive, unambiguous int64 values. Textual inputs must
// consist entirely of decimal digits; signs, whitespace, fractions and
// exponent notation are deliberately rejected. A float64 is accepted only
// when it is finite, integral and strictly below 2^63.
func Parse(value any) int64 {
	var parsed int64
	switch typed := value.(type) {
	case int64:
		parsed = typed
	case int:
		parsed = int64(typed)
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) || typed <= 0 || math.Trunc(typed) != typed || typed >= math.Exp2(63) {
			return 0
		}
		parsed = int64(typed)
	case json.Number:
		return parseDecimal(typed.String())
	case string:
		return parseDecimal(typed)
	default:
		return 0
	}
	if parsed <= 0 {
		return 0
	}
	return parsed
}

func parseDecimal(raw string) int64 {
	if raw == "" {
		return 0
	}
	for _, char := range raw {
		if char < '0' || char > '9' {
			return 0
		}
	}
	parsed, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || parsed <= 0 {
		return 0
	}
	return parsed
}
