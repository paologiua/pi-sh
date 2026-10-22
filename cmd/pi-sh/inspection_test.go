package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOfflineInspectionHelper(t *testing.T) {
	if os.Getenv("PI_SH_INSPECTION_HELPER") != "1" {
		return
	}
	for i, arg := range os.Args {
		if arg == "--" {
			os.Args = append([]string{"pi-sh"}, os.Args[i+1:]...)
			os.Exit(app())
		}
	}
	os.Exit(254)
}

func runInspection(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	command := exec.Command(os.Args[0], append([]string{"-test.run=^TestOfflineInspectionHelper$", "--"}, args...)...)
	command.Env = append(os.Environ(), "PI_SH_INSPECTION_HELPER=1", "GORACE=atexit_sleep_ms=0")
	var out, diagnostic bytes.Buffer
	command.Stdout = &out
	command.Stderr = &diagnostic
	err := command.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return code, out.String(), diagnostic.String()
}

func TestOfflineVersionDoesNotLoadConfigOrAuthenticate(t *testing.T) {
	for _, args := range [][]string{{"-V"}, {"-nVxz"}, {"-G", "-V"}, {"-F", filepath.Join(t.TempDir(), "missing-private-config"), "-V"}, {"--browser", "--direct-login", "-V", "-z"}} {
		code, out, diagnostic := runInspection(t, args...)
		if code != 0 || out != "" || !strings.HasPrefix(diagnostic, "pi-sh_") || !strings.Contains(diagnostic, "unofficial") || strings.Count(diagnostic, "\n") != 1 {
			t.Fatalf("%q: code%d out%q err%q", args, code, out, diagnostic)
		}
	}
}

func TestOfflineCapabilityQueries(t *testing.T) {
	expected := map[string]string{
		"help":            "help\npi-sh-auth\npi-sh-command\npi-sh-config\npi-sh-escape\npi-sh-transport\n",
		"pi-sh-auth":      "browser-companion\ndirect-login\n",
		"pi-sh-command":   "experimental-command-no-tty\nexperimental-command-tty\ninteractive-shell\nlegacy-command-pty\n",
		"pi-sh-config":    "EscapeChar\nHost\nHostName\nInclude\nLogLevel\nMatch\nRequestTTY\nStdinNull\nSyslogFacility\nTag\n",
		"pi-sh-escape":    "disconnect\nhelp\nliteral-prefix\n",
		"pi-sh-transport": "connect-webrtc\n",
	}
	if runtime.GOOS == "darwin" || runtime.GOOS == "linux" {
		expected["pi-sh-escape"] += "suspend\n"
	}
	for query, want := range expected {
		t.Run(query, func(t *testing.T) {
			for _, args := range [][]string{{"-Q", query}, {"-Q" + query, "-z"}, {"-F", filepath.Join(t.TempDir(), "missing-private-config"), "-Q", query, "-V"}} {
				code, out, diagnostic := runInspection(t, args...)
				if code != 0 || out != want || diagnostic != "" {
					t.Fatalf("%q: code%d out%q err%q", args, code, out, diagnostic)
				}
			}
		})
	}
}

func TestOfflineUnsupportedQueriesAreRedactedAndNeverListSSHAlgorithms(t *testing.T) {
	for _, query := range []string{"cipher", "key", "protocol-version", "Ciphers", "PRIVATE-QUERY-SENTINEL", "\x1bPRIVATE-QUERY-SENTINEL", ""} {
		code, out, diagnostic := runInspection(t, "-Q", query, "-V")
		if code != 255 || out != "" || !strings.Contains(diagnostic, "-Q help") || strings.Contains(diagnostic, "PRIVATE-QUERY-SENTINEL") {
			t.Fatalf("code%d out%q err%q", code, out, diagnostic)
		}
	}
}

func TestOfflineInspectionOrderMatchesOpenSSH(t *testing.T) {
	ssh, err := exec.LookPath("ssh")
	if err != nil {
		t.Skip("local OpenSSH unavailable")
	}
	for _, args := range [][]string{{"-V"}, {"-V", "-z"}, {"-z", "-V"}, {"-F", filepath.Join(t.TempDir(), "missing-config"), "-V"}, {"-G", "-V"}, {"-Qhelp", "-z"}, {"-V", "-Qhelp"}, {"-Qhelp", "-V"}, {"-QV"}, {"-Q"}} {
		command := exec.Command(ssh, args...)
		var out, diagnostic bytes.Buffer
		command.Stdout = &out
		command.Stderr = &diagnostic
		err := command.Run()
		wantCode := 0
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				wantCode = exit.ExitCode()
			} else {
				t.Fatal(err)
			}
		}
		code, gotOut, gotErr := runInspection(t, args...)
		if code != wantCode || (gotOut != "") != (out.Len() != 0) || (gotErr != "") != (diagnostic.Len() != 0) {
			t.Fatalf("%q: pi-sh code%d stdout%t stderr%t; reference code%d stdout%t stderr%t", args, code, gotOut != "", gotErr != "", wantCode, out.Len() != 0, diagnostic.Len() != 0)
		}
	}
}

type inspectionFailedWriter struct{ short bool }

func (w inspectionFailedWriter) Write(b []byte) (int, error) {
	if w.short {
		return len(b) - 1, nil
	}
	return 0, errors.New("PRIVATE-WRITER-SENTINEL")
}
func TestInspectionWriteFailureIsNotSuccessOrPrivateDiagnostic(t *testing.T) {
	for _, o := range []options{{inspection: "version"}, {inspection: "query", query: "pi-sh-command"}} {
		for _, short := range []bool{false, true} {
			writer := inspectionFailedWriter{short: short}
			err := writeInspection(o, writer, writer)
			if err == nil || strings.Contains(err.Error(), "PRIVATE-WRITER-SENTINEL") {
				t.Fatal("local information write failure lost or exposed", err)
			}
		}
	}
}

func TestInspectionRejectsInvalidPriorOptionsBeforeEarlyExit(t *testing.T) {
	for _, args := range [][]string{{"-z", "-V"}, {"-oUnknownPrivateOption=PRIVATE-OPTION-SENTINEL", "-V"}, {"-oRequestTTY=invalid", "-Qhelp"}, {"-Q"}, {"-Q", "-V"}} {
		code, out, diagnostic := runInspection(t, args...)
		if code != 255 || out != "" || diagnostic == "" || strings.Contains(diagnostic, "PRIVATE-OPTION-SENTINEL") {
			t.Fatalf("code%d out%q err%q", code, out, diagnostic)
		}
	}
}

func TestInspectionFlagsAfterCommandStartRemainCommandOperands(t *testing.T) {
	o, err := parseOptions([]string{"--browser", "--experimental-command", "device", "printf", "-V", "-Qhelp"})
	if err != nil || o.inspection != "" || strings.Join(o.args, "|") != "device|printf|-V|-Qhelp" {
		t.Fatalf("command operands changed: %+v %v", o, err)
	}
}
