package main

import (
	"flag"
	"golang.org/x/term"
	"os"
	"pi-sh/internal/progress"
)

type progressQuietFlag struct {
	flag.Value
	disabled *bool
}

// String must work on the zero value: flag.PrintDefaults inspects it.
func (f progressQuietFlag) String() string { return "" }

func (f progressQuietFlag) IsBoolFlag() bool { return true }
func (f progressQuietFlag) Set(value string) error {
	if err := f.Value.Set(value); err != nil {
		return err
	}
	*f.disabled = true
	return nil
}

func sftpProgress(output, diagnostics *os.File) *progress.Display {
	if diagnostics == nil || !term.IsTerminal(int(diagnostics.Fd())) {
		return nil
	}
	return progress.NewTerminal(output, false)
}
