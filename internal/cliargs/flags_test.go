package cliargs

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

type values []string

func (v *values) String() string     { return "" }
func (v *values) Set(s string) error { *v = append(*v, s); return nil }

func TestShortArgumentsAndOperandBoundaries(t *testing.T) {
	for _, args := range [][]string{
		{"--direct-login", "-Fnone", "-b-", "-s/existing server", "-oHostName=Pi", "-o", "RequestTTY=no", "device", "-Foperand"},
		{"--direct-login=true", "-F", "none", "-b", "-", "-s", "/existing server", "-o", "HostName=Pi", "-oRequestTTY=no", "device", "-Foperand"},
	} {
		f := flag.NewFlagSet("test", flag.ContinueOnError)
		f.SetOutput(io.Discard)
		direct := f.Bool("direct-login", false, "")
		config, batch, server := f.String("F", "", ""), f.String("b", "", ""), f.String("s", "", "")
		var options values
		f.Var(&options, "o", "")
		if err := Parse(f, args); err != nil {
			t.Fatal(err)
		}
		if !*direct || *config != "none" || *batch != "-" || *server != "/existing server" || !reflect.DeepEqual(options, values{"HostName=Pi", "RequestTTY=no"}) || !reflect.DeepEqual(f.Args(), []string{"device", "-Foperand"}) {
			t.Fatal("option/operand boundaries changed")
		}
	}
}

func TestLiteralValueAndDoubleDash(t *testing.T) {
	for _, tt := range []struct {
		args     []string
		want     string
		operands []string
	}{
		{[]string{"-F=literal", "--", "-device"}, "=literal", []string{"-device"}},
		{[]string{"-F", "-oNotAnOption", "device"}, "-oNotAnOption", []string{"device"}},
		{[]string{"--F=literal", "device"}, "literal", []string{"device"}},
		{[]string{"-F", "--", "device"}, "--", []string{"device"}},
	} {
		f := flag.NewFlagSet("test", flag.ContinueOnError)
		f.SetOutput(io.Discard)
		config := f.String("F", "", "")
		if err := Parse(f, tt.args); err != nil {
			t.Fatal(err)
		}
		if *config != tt.want || !reflect.DeepEqual(f.Args(), tt.operands) {
			t.Fatalf("%q: %q %q", tt.args, *config, f.Args())
		}
	}
}

func TestUnsupportedOptionsAndMissingArguments(t *testing.T) {
	for _, args := range [][]string{{"-F"}, {"-o"}, {"--sftp-server"}, {"-r", "source", "device:target"}, {"-p"}, {"-P22"}, {"--unknown=secret"}, {"-f=false"}, {"-f=true"}} {
		f := flag.NewFlagSet("test", flag.ContinueOnError)
		f.SetOutput(io.Discard)
		f.String("F", "", "")
		f.String("o", "", "")
		f.String("sftp-server", "", "")
		f.Bool("f", false, "")
		if err := Parse(f, args); err == nil {
			t.Fatalf("invalid option accepted: %q", args)
		}
	}
}

func TestLongValueIsNeverReparsed(t *testing.T) {
	f := flag.NewFlagSet("test", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	f.String("F", "", "")
	server := f.String("sftp-server", "", "")
	if err := Parse(f, []string{"--sftp-server", "-Fmust-remain-a-value", "device"}); err != nil || *server != "-Fmust-remain-a-value" {
		t.Fatal("long value parsed as options", err)
	}
}

func TestRegisteredSwitchClustersRetainArgumentAndLongBoundaries(t *testing.T) {
	for _, args := range [][]string{{"-ffb-", "device"}, {"-f", "-b-", "device"}, {"-fb", "-", "device"}, {"-browser", "-ffb-", "device"}} {
		f := flag.NewFlagSet("test", flag.ContinueOnError)
		f.SetOutput(io.Discard)
		flush := f.Bool("f", false, "")
		batch := f.String("b", "", "")
		browser := f.Bool("browser", false, "")
		if err := Parse(f, args); err != nil || !*flush || *batch != "-" || len(f.Args()) != 1 || f.Args()[0] != "device" || *browser != (args[0] == "-browser") {
			t.Fatal("switch cluster boundaries", args, err)
		}
	}
}
