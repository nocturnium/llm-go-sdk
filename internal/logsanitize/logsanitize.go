// Package logsanitize provides helpers for making untrusted values safe to log.
package logsanitize

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Value returns s with control characters escaped so untrusted values cannot
// inject forged log lines or terminal control sequences.
func Value(s string) string {
	var b strings.Builder
	changed := false
	copyFrom := 0

	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			i++
			continue
		}

		replacement, ok := escapedControl(r)
		if !ok {
			i += size
			continue
		}

		if !changed {
			b.Grow(len(s))
			changed = true
		}
		b.WriteString(s[copyFrom:i])
		b.WriteString(replacement)
		i += size
		copyFrom = i
	}

	if !changed {
		return s
	}
	b.WriteString(s[copyFrom:])
	return b.String()
}

func escapedControl(r rune) (string, bool) {
	switch r {
	case '\r':
		return `\r`, true
	case '\n':
		return `\n`, true
	case '\t':
		return `\t`, true
	}
	if r < 0x20 || r == 0x7f {
		return fmt.Sprintf(`\x%02x`, r), true
	}
	if unicode.IsControl(r) {
		if r <= 0xffff {
			return fmt.Sprintf(`\u%04x`, r), true
		}
		return fmt.Sprintf(`\U%08x`, r), true
	}
	return "", false
}
