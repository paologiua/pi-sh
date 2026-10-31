package cliargs

import (
	"bytes"
	"flag"
	"strings"
	"testing"
)

func TestPrintOptionsUsesCanonicalSpellingAndParserInventory(t *testing.T) {
	f := flag.NewFlagSet("test", flag.ContinueOnError)
	var output bytes.Buffer
	f.SetOutput(&output)
	f.Bool("direct-login", false, "direct Connect login")
	f.Bool("q", false, "disable progress")
	version := f.Bool("version", false, "Print client version")
	f.BoolVar(version, "V", false, "Print client version")
	f.String("F", "none", "explicit configuration")
	f.String("sftp-server", "", "existing remote server")
	Refuse(f, map[string]string{"A": "agent forwarding unavailable"}, map[string]string{"i": "keys unavailable"})
	PrintOptions(f)
	for _, required := range []string{"--direct-login", "--sftp-server path", "-q", "-F configfile", "-i identity_file", "not supported: keys unavailable", "-h, --help", "-V, --version"} {
		if !strings.Contains(output.String(), required) {
			t.Errorf("help missing %q: %s", required, output.String())
		}
	}
	for _, line := range strings.Split(output.String(), "\n") {
		if len(line) > 96 {
			t.Errorf("help line too wide: %q", line)
		}
	}
	if strings.Contains(output.String(), "panic") || strings.Contains(output.String(), "  -direct-login") {
		t.Fatalf("invalid help: %s", output.String())
	}
}
