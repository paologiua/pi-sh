//go:build !(darwin || linux)

package fwdtest

// Setsid is unavailable outside macOS and Linux.
func Setsid([]string) int { return 125 }
