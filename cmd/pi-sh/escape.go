package main

import (
	"fmt"

	"pi-sh/internal/sshconfig"
)

// The input path carries UTF-8 text, so byte-valued escape characters outside
// ASCII cannot be supported without inventing a binary-input mapping.
func parseEscape(value string) (rune, error) {
	return sshconfig.Escape(value)
}

func escapeLabel(c rune) string {
	if c < 32 {
		return fmt.Sprintf("^%c", c+64)
	}
	if c == 127 {
		return "^?"
	}
	return string(c)
}
