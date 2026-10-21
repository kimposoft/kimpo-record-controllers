package recordversion

import (
	"encoding/json"
	"math"
	"testing"
)

func TestParseStrictPositiveInt64(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  int64
	}{
		{name: "float whole", value: float64(7), want: 7},
		{name: "int64", value: int64(9), want: 9},
		{name: "int", value: int(11), want: 11},
		{name: "json integer", value: json.Number("12"), want: 12},
		{name: "string integer", value: "13", want: 13},
		{name: "int64 max", value: "9223372036854775807", want: math.MaxInt64},
		{name: "fractional float", value: 7.5},
		{name: "fractional json", value: json.Number("7.5")},
		{name: "fractional string", value: "7.5"},
		{name: "scientific json", value: json.Number("7e0")},
		{name: "scientific string", value: "7e0"},
		{name: "trailing", value: "7x"},
		{name: "leading whitespace", value: " 7"},
		{name: "trailing whitespace", value: "7 "},
		{name: "plus sign", value: "+7"},
		{name: "overflow", value: "9223372036854775808"},
		{name: "float upper bound", value: math.Exp2(63)},
		{name: "negative float", value: float64(-7)},
		{name: "negative string", value: "-7"},
		{name: "zero", value: 0},
		{name: "nan", value: math.NaN()},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "negative infinity", value: math.Inf(-1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Parse(test.value); got != test.want {
				t.Fatalf("Parse(%#v) = %d, want %d", test.value, got, test.want)
			}
		})
	}
}
