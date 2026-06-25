package deploy

import "strings"

// Sanitize strips ANSI/VT escape sequences from framework output and trims
// surrounding whitespace, so command output is safe to store in audit logs and
// Result.Output. C2 operator clients (Sliver, Mythic, Metasploit, …) share this
// rather than each carrying a copy.
func Sanitize(s string) string {
	var b strings.Builder
	inEscape := false
	for _, r := range s {
		if r == '\x1b' {
			inEscape = true
			continue
		}
		if inEscape {
			if r == 'm' {
				inEscape = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
