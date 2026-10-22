package diag

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParseLevelAndFacility(t *testing.T) {
	for in, want := range map[string]string{
		"quiet": "QUIET", "FATAL": "FATAL", "Error": "ERROR", "info": "INFO", "VERBOSE": "VERBOSE",
		"debug": "DEBUG", "DEBUG1": "DEBUG", "debug2": "DEBUG2", "DeBuG3": "DEBUG3",
	} {
		l, err := ParseLevel(in)
		if err != nil || l.String() != want {
			t.Errorf("%q: %v %v", in, l, err)
		}
	}
	// OpenSSH rejects SILENT and DEBUG4; Unicode case folding is not ASCII.
	for _, in := range []string{"", "SILENT", "DEBUG4", "bogus", "ınfo", "INFO ", "debug0"} {
		if _, err := ParseLevel(in); err == nil {
			t.Errorf("accepted %q", in)
		}
	}
	for _, in := range []string{"user", "DAEMON", "Auth", "AUTHPRIV", "local0", "LOCAL7"} {
		f, err := ParseFacility(in)
		if err != nil || f.String() != strings.ToUpper(in) {
			t.Errorf("%q: %v %v", in, f, err)
		}
	}
	for _, in := range []string{"", "LOCAL8", "KERN", "syslog", "bogus"} {
		if _, err := ParseFacility(in); err == nil {
			t.Errorf("accepted facility %q", in)
		}
	}
}

// Expected values were measured with OpenSSH_10.2p1 `ssh -G` (see
// cmd/pi-sh/logging_test.go for the live differential).
func TestVerbosityMatchesOpenSSHOrdering(t *testing.T) {
	for seq, want := range map[string]string{
		"": "INFO", "v": "DEBUG", "vv": "DEBUG2", "vvv": "DEBUG3", "vvvvv": "DEBUG3",
		"q": "QUIET", "qq": "QUIET", "qv": "DEBUG", "vq": "QUIET", "vqv": "FATAL", "vqvv": "ERROR",
	} {
		var v Verbosity
		for _, c := range seq {
			if c == 'v' {
				v.Verbose()
			} else {
				v.Quiet()
			}
		}
		got, err := v.Resolve("ERROR")
		if seq == "" {
			got, err = v.Resolve("")
		}
		if err != nil || got.String() != want {
			t.Errorf("%q: got %v %v want %s", seq, got, err, want)
		}
	}
	var v Verbosity
	if l, _ := v.Resolve("debug3"); l != Debug3 {
		t.Fatal("directive ignored without flags")
	}
	if _, err := v.Resolve("bogus"); err == nil {
		t.Fatal("invalid directive accepted")
	}
}

func TestLevelFilteringAndFormat(t *testing.T) {
	for _, level := range []Level{Quiet, Fatal, Error, Info, Verbose, Debug1, Debug2, Debug3} {
		var out bytes.Buffer
		l, err := Open(Config{Program: "pi-sh", Level: level, Stderr: &out})
		if err != nil {
			t.Fatal(err)
		}
		l.Fatal("fatal")
		l.Error("error")
		l.Info("info")
		l.Verbose("verbose")
		l.Debug("one", Count("n", 2))
		l.Debug2("two", State("state", "connected"))
		l.Debug3("three", Elapsed("elapsed", 1500*time.Millisecond), Bool("ok", true))
		all := []string{"pi-sh: fatal\r\n", "pi-sh: error\r\n", "pi-sh: info\r\n", "pi-sh: verbose\r\n",
			"debug1: one n=2\r\n", "debug2: two state=connected\r\n", "debug3: three elapsed=1500ms ok=true\r\n"}
		if want := strings.Join(all[:level], ""); out.String() != want {
			t.Errorf("%v: got %q want %q", level, out.String(), want)
		}
	}
	var nilLogger *Logger
	nilLogger.Info("discarded")
	nilLogger.Failure(Fatal, errors.New("x"))
	if nilLogger.Enabled(Fatal) || nilLogger.Close() != nil {
		t.Fatal("nil logger must discard")
	}
}

// Secrets must never reach any diagnostic destination, whether supplied as
// error text, a state field or (defensively) a message.
func TestRedaction(t *testing.T) {
	secrets := []string{
		"hunter2-password", "TOTP-123456", "cookie-value-abc", "eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl",
		"0123456789abcdef0123456789abcdef", "ufragXYZ", "icepwdSECRET", "owner@example.com",
		"capability-path-7f3a", "turnuser", "relay-pass", "198.51.100.7",
	}
	inputs := []string{
		"login failed password=hunter2-password",
		"code: TOTP-123456 rejected; otp=TOTP-123456",
		"Set-Cookie: session=cookie-value-abc; Path=/",
		"Authorization: Bearer eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.c2lnbmF0dXJl",
		"device token 0123456789abcdef0123456789abcdef",
		"v=0\r\no=- 1 2 IN IP4 127.0.0.1\r\na=ice-ufrag:ufragXYZ\r\na=ice-pwd:icepwdSECRET\r\n",
		"answer a=candidate:1 1 udp 2122260223 198.51.100.7 54321 typ host",
		"user owner@example.com signed in",
		"open http://127.0.0.1:43210/capability-path-7f3a?x=1 failed",
		"ice turn:turnuser:relay-pass@relay.example:3478 unreachable",
		"fetch(\"https://connect.example/x?token=abc\")",
	}
	var out bytes.Buffer
	l, err := Open(Config{Program: "pi-sh", Level: Debug3, Stderr: &out})
	if err != nil {
		t.Fatal(err)
	}
	for _, in := range inputs {
		l.Failure(Fatal, errors.New(in))
		l.Log(Debug1, Message(in)) // defensive: even a misused message is sanitized
		l.Debug("state", State("value", in))
	}
	for _, s := range secrets {
		if strings.Contains(out.String(), s) {
			t.Errorf("secret %q leaked:\n%s", s, out.String())
		}
	}
	if strings.Count(out.String(), "\r\n") != 3*len(inputs) {
		t.Fatalf("multi-line input split a diagnostic line:\n%q", out.String())
	}
	for in, want := range map[string]string{
		"remote connection closed; Connect has not supplied a remote exit status": "remote connection closed; Connect has not supplied a remote exit status",
		"\x1b]0;title\x07colour\x1b[31m":                                          "?]0;title?colour?[31m",
		"bidi‮evil":                                                               "bidi?evil",
		"bad\xffutf8":                                                             "bad?utf8",
		"select --browser or --direct-login":                                      "select --browser or --direct-login",
		"session authorization check failed: x":                                   "session authorization check failed: x",
		"unsupported SSH configuration directive":                                 "unsupported SSH configuration directive",
		"WebRTC message exceeds local limit":                                      "WebRTC message exceeds local limit",
		"pi-sh_devel go1.27.3 linux/386 (unofficial)":                             "pi-sh_devel go1.27.3 linux/386 (unofficial)",
	} {
		if got := Sanitize(in); got != want {
			t.Errorf("Sanitize(%q) = %q want %q", in, got, want)
		}
	}
	if got := Sanitize(strings.Repeat("é", MaxLine)); len(got) > MaxLine || !strings.HasPrefix(got, "é") {
		t.Fatal("line bound broke UTF-8 or limit")
	}
	if State("k", "connected").value != "connected" || State("k", "has space").value != "[redacted]" || State("bad key", "x").key != "field" {
		t.Fatal("state token policy")
	}
}

func TestLogFileAppendsPrivately(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "client.log")
	if err := os.WriteFile(path, []byte("previous\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	l, err := Open(Config{Program: "pi-sh", Level: Info, Stderr: &stderr, File: path, UseFile: true})
	if err != nil {
		t.Fatal(err)
	}
	l.Info("hello")
	l.Debug("hidden")
	if err := l.Close(); err != nil {
		t.Fatal(err)
	}
	l.Info("after close")
	b, _ := os.ReadFile(path)
	if string(b) != "previous\npi-sh: hello\r\n" || stderr.Len() != 0 {
		t.Fatalf("log file %q stderr %q", b, stderr.String())
	}
	fresh := filepath.Join(dir, "new.log")
	l, err = Open(Config{Program: "pi-sh", Level: Info, File: fresh, UseFile: true})
	if err != nil {
		t.Fatal(err)
	}
	l.Close()
	info, err := os.Stat(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("new log file is not private: %v", info.Mode())
	}
	for _, bad := range []string{"", filepath.Join(dir, "missing", "x.log"), dir} {
		_, err := Open(Config{File: bad, UseFile: true})
		if err == nil || strings.Contains(err.Error(), dir) {
			t.Errorf("open %q: %v", bad, err)
		}
	}
	if _, err := Open(Config{File: fresh, UseFile: true, Syslog: true}); !errors.Is(err, ErrBothDestinations) {
		t.Fatal("-y with -E accepted")
	}
	var pathErr *fs.PathError
	if errors.As(fileError(&fs.PathError{Op: "open", Path: "secret", Err: fs.ErrPermission}), &pathErr) {
		t.Fatal("path error leaked")
	}
}

func TestConcurrentLogging(t *testing.T) {
	var out lockedBuffer
	l := Default(&out, "pi-sh")
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); l.Info("line") }()
	}
	wg.Wait()
	if strings.Count(out.String(), "pi-sh: line\r\n") != 16 {
		t.Fatal("interleaved output")
	}
}

type lockedBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.b.Write(p)
}
func (b *lockedBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

// Structural guarantee: production code never converts a runtime string to a
// Message. Dynamic text can only reach the logger through Failure (sanitized)
// or typed fields.
func TestNoDynamicMessages(t *testing.T) {
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			name := ""
			switch fun := call.Fun.(type) {
			case *ast.SelectorExpr:
				if x, ok := fun.X.(*ast.Ident); ok && x.Name == "diag" {
					name = fun.Sel.Name
				}
			case *ast.Ident:
				if file.Name.Name == "diag" {
					name = fun.Name
				}
			}
			if name == "Message" {
				if _, lit := call.Args[0].(*ast.BasicLit); !lit {
					t.Errorf("%s: dynamic diag.Message conversion", fset.Position(call.Pos()))
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
