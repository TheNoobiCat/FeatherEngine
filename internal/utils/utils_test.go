package utils

import (
	"testing"
)

func TestWei18(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{0.0, "0"},
		{1.0, "1000000000000000000"},
		{0.5, "500000000000000000"},
		{123.456, "123456000000000000000"},
	}

	for _, test := range tests {
		result := Wei18(test.input)
		if result.String() != test.expected {
			t.Errorf("Wei18(%f) = %s, want %s", test.input, result.String(), test.expected)
		}
	}
}
