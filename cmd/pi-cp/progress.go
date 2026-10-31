package main

import "flag"

// scp's showprogress stays false after any -q, independently of subsequent -v
// flags or connection-layer LogLevel options.
type progressQuietFlag struct {
	flag.Value
	quiet *bool
}

// String must work on the zero value: flag.PrintDefaults inspects it.
func (f progressQuietFlag) String() string { return "" }

func (f progressQuietFlag) IsBoolFlag() bool { return true }
func (f progressQuietFlag) Set(value string) error {
	if err := f.Value.Set(value); err != nil {
		return err
	}
	*f.quiet = true
	return nil
}
