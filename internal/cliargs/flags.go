// Package cliargs adapts supported short options with arguments to flag.FlagSet.
// It does not enable unimplemented options or reinterpret operands as flags.
package cliargs

import (
	"flag"
	"fmt"
	"strings"
)

type boolFlag interface{ IsBoolFlag() bool }

func takesValue(f *flag.Flag) bool {
	if f == nil {
		return false
	}
	b, ok := f.Value.(boolFlag)
	return !ok || !b.IsBoolFlag()
}

// Parse accepts both -F file and -Ffile for registered single-letter options
// requiring a value, and clusters of registered short switches. An attached '='
// belongs to a short argument's value, as in OpenSSH.
// Long options retain flag's spelling and =value behavior. Options end at -- or
// the first operand. A value beginning with '-' is consumed without re-parsing.
func Parse(f *flag.FlagSet, args []string) error {
	normalized := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || arg == "-" || !strings.HasPrefix(arg, "-") {
			normalized = append(normalized, args[i:]...)
			break
		}
		name := strings.TrimPrefix(arg, "-")
		long := strings.HasPrefix(name, "-")
		name = strings.TrimPrefix(name, "-")
		name, _, equal := strings.Cut(name, "=")
		known := f.Lookup(name)
		if known != nil && len(name) == 1 && !takesValue(known) && equal {
			err := fmt.Errorf("short option -%s does not take a value", name)
			fmt.Fprintln(f.Output(), err)
			return err
		}
		// Preserve existing single-dash long options accepted by Go flag.
		if !long && len(arg) > 2 && (known == nil || len(name) == 1) {
			for j := 1; j < len(arg); j++ {
				short := f.Lookup(arg[j : j+1])
				if short == nil {
					return f.Parse(append(normalized, args[i:]...))
				}
				normalized = append(normalized, "-"+arg[j:j+1])
				if takesValue(short) {
					if j+1 < len(arg) {
						normalized = append(normalized, arg[j+1:])
					} else if i+1 < len(args) {
						i++
						normalized = append(normalized, args[i])
					}
					break
				}
			}
			continue
		}
		normalized = append(normalized, arg)
		if known == nil {
			// Let flag report the original unknown option before any later args.
			normalized = append(normalized, args[i+1:]...)
			break
		}
		if takesValue(known) && !equal && i+1 < len(args) {
			i++
			normalized = append(normalized, args[i])
		}
	}
	return f.Parse(normalized)
}
