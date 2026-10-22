package diag

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Message is fixed, secret-free phase text. Call sites pass constants; a
// conversion from a runtime string is rejected by TestNoDynamicMessages.
type Message string

// Field is a typed diagnostic value. Constructors admit only numbers, durations,
// booleans and short enumerated tokens, never free-form or payload text.
type Field struct{ key, value string }

func fieldKey(k string) string {
	if !tokenSyntax.MatchString(k) || len(k) > 32 {
		return "field"
	}
	return k
}

func Count(key string, n int) Field { return Field{fieldKey(key), strconv.Itoa(n)} }
func Bool(key string, b bool) Field { return Field{fieldKey(key), strconv.FormatBool(b)} }
func Elapsed(key string, d time.Duration) Field {
	return Field{fieldKey(key), strconv.FormatInt(d.Milliseconds(), 10) + "ms"}
}

// State records an enumerated value (a WebRTC state, login stage constant or
// level name). Values that are not short identifier tokens are redacted.
func State(key, value string) Field { return Field{fieldKey(key), stateToken(value)} }

// Version records a locally built version string (client release, Go
// toolchain). It admits '+' build metadata and pseudo-version hashes that
// State would redact; anything outside version syntax is still redacted.
func Version(key, value string) Field { return Field{fieldKey(key), versionToken(value)} }

// Config selects one destination: Stderr by default, File for -E (append,
// created 0600) or Syslog for -y. Both File and Syslog is an error, as in ssh.
type Config struct {
	Program  string
	Level    Level
	Stderr   io.Writer
	File     string
	UseFile  bool
	Syslog   bool
	Facility Facility

	// syslogNetwork/syslogAddr override the local syslog socket in tests.
	syslogNetwork, syslogAddr string
}

type sysLogger interface {
	write(Level, string) error
	Close() error
}

// Logger is safe for concurrent use. A nil *Logger discards everything.
type Logger struct {
	mu      sync.Mutex
	program string
	level   Level
	out     io.Writer
	file    *os.File
	sys     sysLogger
}

var (
	ErrBothDestinations = errors.New("cannot specify both -y and -E")
	ErrSyslog           = errors.New("cannot connect to the local syslog service")
)

// Open creates the configured logger. Log file errors are reported by class
// only (the path and OS error text are not echoed).
func Open(c Config) (*Logger, error) {
	if c.Syslog && c.UseFile {
		return nil, ErrBothDestinations
	}
	l := &Logger{program: c.Program, level: c.Level, out: c.Stderr}
	if l.out == nil {
		l.out = io.Discard
	}
	switch {
	case c.UseFile:
		f, err := os.OpenFile(c.File, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o600)
		if err != nil {
			return nil, fileError(err)
		}
		l.file, l.out = f, f
	case c.Syslog:
		s, err := openSyslog(c.syslogNetwork, c.syslogAddr, c.Facility, c.Program)
		if err != nil {
			return nil, err
		}
		l.sys = s
	}
	return l, nil
}

func fileError(err error) error {
	reason := "error"
	switch {
	case errors.Is(err, fs.ErrNotExist):
		reason = "no such file or directory"
	case errors.Is(err, fs.ErrPermission):
		reason = "permission denied"
	default:
		var pathErr *fs.PathError
		if errors.As(err, &pathErr) && strings.Contains(strings.ToLower(pathErr.Err.Error()), "directory") {
			reason = "is a directory"
		}
	}
	return errors.New("cannot open log file: " + reason)
}

// Default writes INFO-level messages to w, for callers without a configured
// logger (for example package tests).
func Default(w io.Writer, program string) *Logger {
	return &Logger{program: program, level: Info, out: w}
}

func (l *Logger) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	var err error
	if l.file != nil {
		err = l.file.Close()
		l.file, l.out = nil, io.Discard
	}
	if l.sys != nil {
		err = errors.Join(err, l.sys.Close())
		l.sys = nil
	}
	return err
}

func (l *Logger) Level() Level {
	if l == nil {
		return Quiet
	}
	return l.level
}

// Enabled reports whether a message at level would be emitted.
func (l *Logger) Enabled(level Level) bool {
	return l != nil && level > Quiet && level <= l.level
}

// Log emits one sanitized line. Debug lines use OpenSSH's "debugN: " prefix;
// others use "program: ". Lines end with CRLF on streams, as in OpenSSH, so
// they stay readable while a local terminal is in raw mode.
func (l *Logger) Log(level Level, m Message, fields ...Field) {
	if !l.Enabled(level) {
		return
	}
	// Structural redaction: the constant message is sanitized (its phase
	// prefix kept), while fields were already restricted by their typed
	// constructors and only get control-character cleanup and the line bound.
	// Running Sanitize over the whole line produced false positives such as
	// "session:[redacted]" and long version strings as "[redacted-token]".
	var b strings.Builder
	b.WriteString(l.prefix(level))
	b.WriteString(sanitizeMessage(string(m)))
	for _, f := range fields {
		b.WriteByte(' ')
		b.WriteString(f.key)
		b.WriteByte('=')
		b.WriteString(f.value)
	}
	l.write(level, clean(b.String()))
}

// Failure logs an error's text after redaction. Error text is produced locally
// and must never embed remote payloads; Sanitize is the backstop.
func (l *Logger) Failure(level Level, err error) {
	if err == nil || !l.Enabled(level) {
		return
	}
	l.write(level, Sanitize(l.prefix(level)+err.Error()))
}

func (l *Logger) prefix(level Level) string {
	switch level {
	case Debug1:
		return "debug1: "
	case Debug2:
		return "debug2: "
	case Debug3:
		return "debug3: "
	}
	if l.program != "" {
		return l.program + ": "
	}
	return ""
}

func (l *Logger) write(level Level, line string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sys != nil {
		_ = l.sys.write(level, line)
		return
	}
	_, _ = io.WriteString(l.out, line+"\r\n")
}

func (l *Logger) Fatal(m Message, f ...Field)   { l.Log(Fatal, m, f...) }
func (l *Logger) Error(m Message, f ...Field)   { l.Log(Error, m, f...) }
func (l *Logger) Info(m Message, f ...Field)    { l.Log(Info, m, f...) }
func (l *Logger) Verbose(m Message, f ...Field) { l.Log(Verbose, m, f...) }
func (l *Logger) Debug(m Message, f ...Field)   { l.Log(Debug1, m, f...) }
func (l *Logger) Debug2(m Message, f ...Field)  { l.Log(Debug2, m, f...) }
func (l *Logger) Debug3(m Message, f ...Field)  { l.Log(Debug3, m, f...) }

type contextKey struct{}

// With attaches a logger so shared session layers can report phases without
// signature changes. From returns nil (a discarding logger) when absent.
func With(ctx context.Context, l *Logger) context.Context {
	return context.WithValue(ctx, contextKey{}, l)
}

func From(ctx context.Context) *Logger {
	l, _ := ctx.Value(contextKey{}).(*Logger)
	return l
}
