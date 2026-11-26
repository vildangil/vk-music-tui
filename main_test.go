package main

import "testing"

func TestNormalizeCode(t *testing.T) {
	for _, c := range []struct {
		in   string
		n    int
		call bool
		want string
	}{
		{"123456", 6, true, "123456"},
		{"+7 (999) 123-45-67", 6, true, "234567"},
		{"+7 (999) 123-45-67", 4, true, "4567"},
		{" 12 34 ", 6, false, "1234"},
		{"79991234567", 0, false, "79991234567"},
	} {
		if got := normalizeCode(c.in, c.n, c.call); got != c.want {
			t.Errorf("normalizeCode(%q,%d,%v)=%q, want %q", c.in, c.n, c.call, got, c.want)
		}
	}
}
