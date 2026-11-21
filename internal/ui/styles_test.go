package ui

import (
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestClean(t *testing.T) {
	cases := map[string]string{
		"❤️ Love ❤️":        "Love ",
		"Привет 👋🏽 мир":     "Привет мир",
		"Café":             "Café",
		"Йогурт":           "Йогурт",
		"Tab\tand\nnewline": "Tab and newline",
		"👨‍👩‍👧 family":      "family",
		"    #":             "    #",
		" VK Music":         " VK Music",
		"a​b­c":             "abc",
		"中文":                "中文",
	}
	for in, want := range cases {
		if got := clean(in); got != want {
			t.Errorf("clean(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFitWidth(t *testing.T) {
	for _, s := range []string{"❤️❤️❤️ очень длинное название", "中文歌曲名字很长很长很长", "abc", "", "ééé"} {
		for w := 1; w < 20; w++ {
			if got := ansi.StringWidth(fit(s, w)); got != w {
				t.Fatalf("fit(%q, %d) has width %d", s, w, got)
			}
		}
	}
}
