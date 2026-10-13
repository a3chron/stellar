package term

import "strings"

// SanitizeForTerminal strips C0/C1 control characters (and DEL) from s,
// keeping plain space and tab. Every caller uses this for a single-line
// string that originated from stellar-hub rather than stellar's own code
// (a draft or theme name, an error message) before printing it, so a
// malicious or simply buggy server response can never inject terminal
// escape sequences or fake a second line of CLI output via an embedded
// newline.
func SanitizeForTerminal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == ' ':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			// C0 controls (including \n, \r) and DEL - drop.
		case r >= 0x80 && r <= 0x9f:
			// C1 controls - drop.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// SanitizeMultiline is SanitizeForTerminal for hub-provided text that is
// allowed to span several lines on purpose - a theme description or version
// notes, say, which keep their author's line breaks. It still strips every
// other C0/C1 control character and DEL (escape sequences, a stray
// carriage return that could overwrite part of a previous line, ...); only
// '\n' and '\t' are let through verbatim.
func SanitizeMultiline(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case r == '\t' || r == ' ' || r == '\n':
			b.WriteRune(r)
		case r < 0x20 || r == 0x7f:
			// C0 controls (including \r) and DEL - drop.
		case r >= 0x80 && r <= 0x9f:
			// C1 controls - drop.
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
