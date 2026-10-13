package term

import "testing"

func TestSanitizeForTerminal(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain text is untouched", "My Theme", "My Theme"},
		{"keeps spaces and tabs", "a\tb c", "a\tb c"},
		{"drops a newline", "line1\nline2", "line1line2"},
		{"drops a carriage return", "a\rb", "ab"},
		{"drops an escape sequence", "\x1b[31mred\x1b[0m", "[31mred[0m"},
		{"drops DEL", "a\x7fb", "ab"},
		{"drops C1 controls", "a\u0085b", "ab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeForTerminal(tc.in); got != tc.want {
				t.Errorf("SanitizeForTerminal(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSanitizeMultiline(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"keeps newlines", "line1\nline2", "line1\nline2"},
		{"keeps tabs", "a\tb", "a\tb"},
		{"drops a carriage return", "a\r\nb", "a\nb"},
		{"drops an escape sequence but keeps the newline", "red\x1b[31m\nreset\x1b[0m", "red[31m\nreset[0m"},
		{"drops DEL", "a\x7fb", "ab"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := SanitizeMultiline(tc.in); got != tc.want {
				t.Errorf("SanitizeMultiline(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
