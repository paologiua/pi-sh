package main

import (
	"io"
	"os"
	"strings"
	"testing"
)

func TestPlanModeAndExecutionValidationStayOffline(t *testing.T) {
	root := "/tmp/pi-sh-probe-" + strings.Repeat("a", 32)
	for _, tc := range []struct {
		args     []string
		code     int
		contains string
	}{
		{[]string{"--root", root}, 0, "Known child entries:"},
		{[]string{"--execute-approved", "--browser", "device"}, 1, "exact approved root"},
		{[]string{"--root", root, "--browser", "device"}, 1, "plan mode does not authenticate"},
		{[]string{"--root", root, "--cleanup-only"}, 1, "plan mode does not authenticate"},
		{[]string{"--root", "/tmp", "--execute-approved", "--browser", "device"}, 1, "scratch root"},
		{[]string{"--root", root, "--execute-approved"}, 1, "authentication mode"},
	} {
		oldArgs, oldOut, oldErr := os.Args, os.Stdout, os.Stderr
		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}
		os.Args = append([]string{"transfer-probe"}, tc.args...)
		os.Stdout, os.Stderr = w, w
		code := app()
		os.Args, os.Stdout, os.Stderr = oldArgs, oldOut, oldErr
		w.Close()
		data, err := io.ReadAll(r)
		r.Close()
		if err != nil || code != tc.code || !strings.Contains(string(data), tc.contains) {
			t.Fatalf("offline gate failed: %d %q %v", code, data, err)
		}
	}
}
