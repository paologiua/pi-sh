package cliargs

import (
	"errors"
	"flag"
)

// refusal is a registered OpenSSH option with an explicit "not supported"
// decision. Parsing it fails with the reason instead of flag's generic
// "flag provided but not defined", and its operand is never interpreted.
type refusal struct {
	reason  string
	boolean bool
}

func (r refusal) String() string   { return "" }
func (r refusal) Set(string) error { return errors.New(r.reason) }
func (r refusal) IsBoolFlag() bool { return r.boolean }
func (r refusal) Get() any         { return nil }

// Refuse registers known scp/sftp options that have no Connect equivalent.
// Switches are boolean; options with an operand still consume it, so the
// refusal names the option rather than misreading its operand.
func Refuse(f *flag.FlagSet, switches, withValue map[string]string) {
	for name, reason := range switches {
		f.Var(refusal{reason: "not supported: " + reason, boolean: true}, name, "not supported: "+reason)
	}
	for name, reason := range withValue {
		f.Var(refusal{reason: "not supported: " + reason}, name, "not supported: "+reason)
	}
}
