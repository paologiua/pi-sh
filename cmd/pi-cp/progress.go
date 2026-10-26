package main

import "flag"

// scp's showprogress stays false after any -q, independently of subsequent -v
// flags or connection-layer LogLevel options.
type progressQuietFlag struct {
	flag.Value
	quiet *bool
}

func (f progressQuietFlag) IsBoolFlag() bool { return true }
func (f progressQuietFlag) Set(value string) error {
	if err := f.Value.Set(value); err != nil {
		return err
	}
	*f.quiet = true
	return nil
}
