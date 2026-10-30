package main

import "flag"

type progressQuietFlag struct {
	flag.Value
	disabled *bool
}

func (f progressQuietFlag) IsBoolFlag() bool { return true }
func (f progressQuietFlag) Set(value string) error {
	if err := f.Value.Set(value); err != nil {
		return err
	}
	*f.disabled = true
	return nil
}
