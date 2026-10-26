package main

import (
	"flag"
	"io"
	"testing"

	"pi-sh/internal/cliargs"
	"pi-sh/internal/diag"
)

func TestProgressQuietStaysDisabledAfterVerbose(t *testing.T) {
	for _, tc := range []struct {
		args  []string
		quiet bool
	}{
		{nil, false}, {[]string{"-v"}, false}, {[]string{"-q"}, true},
		{[]string{"-qv"}, true}, {[]string{"-vqv"}, true}, {[]string{"-vvv", "-q", "-v"}, true},
	} {
		var verbosity diag.Verbosity
		quiet := false
		f := flag.NewFlagSet("pi-cp", flag.ContinueOnError)
		f.SetOutput(io.Discard)
		f.Var(verbosity.VerboseFlag(), "v", "")
		f.Var(progressQuietFlag{verbosity.QuietFlag(), &quiet}, "q", "")
		if err := cliargs.Parse(f, tc.args); err != nil || quiet != tc.quiet {
			t.Fatalf("%q: quiet=%t, %v", tc.args, quiet, err)
		}
	}
}
