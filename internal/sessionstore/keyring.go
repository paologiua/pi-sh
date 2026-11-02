package sessionstore

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// Keyring optionally remembers the sessions passphrase (never session data)
// in the operating system keyring, like Apple's UseKeychain for OpenSSH. Get
// reports ok=false, with a nil error, when nothing is stored.
type Keyring interface {
	Get() (passphrase []byte, ok bool, err error)
	Set(passphrase []byte) error
	Delete() error
}

// Keyring item identity. The stored secret is the hex encoding of the
// passphrase, which avoids quoting and encoding problems in the tools.
const (
	keyringService = "pi-sh"
	keyringAccount = "sessions"
	keyringLabel   = "pi-sh sessions"
	securityPath   = "/usr/bin/security"
	keyringTimeout = 2 * time.Minute // the system may ask the user to allow access
)

// ErrNoKeyring means no supported system keyring tool is available.
var ErrNoKeyring = errors.New("no supported system keyring")

// runResult is the outcome of a keyring tool run. Stderr is only inspected
// for emptiness and never included in errors.
type runResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
}

// runFunc runs name with args, feeding stdin. err is set only when the
// program could not be run at all. Secrets are passed on stdin, never in
// args. Tests replace it with a fake.
type runFunc func(ctx context.Context, name string, args []string, stdin []byte) (runResult, error)

func execRun(ctx context.Context, name string, args []string, stdin []byte) (runResult, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdin = bytes.NewReader(stdin)
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return runResult{out.Bytes(), errOut.Bytes(), exit.ExitCode()}, nil
	}
	if err != nil {
		return runResult{}, err
	}
	return runResult{out.Bytes(), errOut.Bytes(), 0}, nil
}

// SystemKeyring returns the keyring of this system: the macOS login
// keychain through /usr/bin/security, or the Secret Service (libsecret)
// through secret-tool on Linux and other Unix systems. Otherwise it returns
// ErrNoKeyring.
func SystemKeyring() (Keyring, error) {
	return systemKeyring(runtime.GOOS, exec.LookPath, execRun)
}

func systemKeyring(goos string, lookPath func(string) (string, error), run runFunc) (Keyring, error) {
	switch goos {
	case "darwin":
		if _, err := lookPath(securityPath); err != nil {
			return nil, fmt.Errorf("%w: %s not found", ErrNoKeyring, securityPath)
		}
		return macKeyring{run: run}, nil
	case "windows", "ios", "android", "js", "wasip1", "plan9":
		return nil, ErrNoKeyring
	default:
		path, err := lookPath("secret-tool")
		if err != nil {
			return nil, fmt.Errorf("%w: secret-tool not found (install libsecret-tools and run a Secret Service such as GNOME Keyring)", ErrNoKeyring)
		}
		return secretToolKeyring{tool: path, run: run}, nil
	}
}

func encodeSecret(passphrase []byte) []byte {
	out := make([]byte, hex.EncodedLen(len(passphrase)))
	hex.Encode(out, passphrase)
	return out
}

func decodeSecret(stdout []byte) ([]byte, error) {
	text := bytes.TrimRight(stdout, "\r\n")
	defer zero(stdout)
	out := make([]byte, hex.DecodedLen(len(text)))
	if _, err := hex.Decode(out, text); err != nil || len(out) == 0 {
		zero(out)
		return nil, errors.New("system keyring item for pi-sh sessions is not in the expected format")
	}
	return out, nil
}

func keyringContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), keyringTimeout)
}

// macKeyring stores a generic password (service pi-sh, account sessions).
type macKeyring struct{ run runFunc }

// securityItemNotFound is errSecItemNotFound as reported by security(1).
const securityItemNotFound = 44

func (k macKeyring) Get() ([]byte, bool, error) {
	ctx, cancel := keyringContext()
	defer cancel()
	r, err := k.run(ctx, securityPath, []string{"find-generic-password", "-s", keyringService, "-a", keyringAccount, "-w"}, nil)
	out, code := r.Stdout, r.ExitCode
	switch {
	case err != nil:
		return nil, false, errors.New("macOS keychain: cannot run security: " + err.Error())
	case code == securityItemNotFound:
		return nil, false, nil
	case code != 0:
		return nil, false, errors.New("macOS keychain: cannot read the pi-sh sessions passphrase")
	}
	pass, err := decodeSecret(out)
	return pass, err == nil, err
}

// Set runs "security -i" and writes the command on its standard input, so
// the secret never appears in any process argument list.
func (k macKeyring) Set(passphrase []byte) error {
	secret := encodeSecret(passphrase)
	defer zero(secret)
	var cmd bytes.Buffer
	cmd.WriteString("add-generic-password -U -s " + keyringService + " -a " + keyringAccount + " -w ")
	cmd.Write(secret)
	cmd.WriteString("\n")
	stdin := cmd.Bytes()
	defer zero(stdin)
	ctx, cancel := keyringContext()
	defer cancel()
	r, err := k.run(ctx, securityPath, []string{"-i"}, stdin)
	if err != nil {
		return errors.New("macOS keychain: cannot run security: " + err.Error())
	}
	// Interactive mode may exit 0 after a failed command; it reports the
	// failure on stderr.
	if r.ExitCode != 0 || len(bytes.TrimSpace(r.Stderr)) != 0 {
		return errors.New("macOS keychain: cannot store the pi-sh sessions passphrase")
	}
	return nil
}

func (k macKeyring) Delete() error {
	ctx, cancel := keyringContext()
	defer cancel()
	r, err := k.run(ctx, securityPath, []string{"delete-generic-password", "-s", keyringService, "-a", keyringAccount}, nil)
	code := r.ExitCode
	switch {
	case err != nil:
		return errors.New("macOS keychain: cannot run security: " + err.Error())
	case code == 0, code == securityItemNotFound:
		return nil
	}
	return errors.New("macOS keychain: cannot delete the pi-sh sessions passphrase")
}

// secretToolKeyring uses libsecret's secret-tool with the attributes
// service=pi-sh account=sessions.
type secretToolKeyring struct {
	tool string
	run  runFunc
}

var secretAttrs = []string{"service", keyringService, "account", keyringAccount}

func (k secretToolKeyring) Get() ([]byte, bool, error) {
	ctx, cancel := keyringContext()
	defer cancel()
	r, err := k.run(ctx, k.tool, append([]string{"lookup"}, secretAttrs...), nil)
	out, code := r.Stdout, r.ExitCode
	switch {
	case err != nil:
		return nil, false, errors.New("secret-tool: " + err.Error())
	case code != 0 && len(out) == 0:
		// secret-tool lookup exits 1 without output when nothing matches.
		return nil, false, nil
	case code != 0:
		zero(out)
		return nil, false, errors.New("secret-tool: cannot read the pi-sh sessions passphrase")
	}
	pass, err := decodeSecret(out)
	return pass, err == nil, err
}

// Set passes the secret on secret-tool's standard input.
func (k secretToolKeyring) Set(passphrase []byte) error {
	secret := encodeSecret(passphrase)
	defer zero(secret)
	ctx, cancel := keyringContext()
	defer cancel()
	r, err := k.run(ctx, k.tool, append([]string{"store", "--label=" + keyringLabel}, secretAttrs...), secret)
	if err != nil {
		return errors.New("secret-tool: " + err.Error())
	}
	if r.ExitCode != 0 {
		return errors.New("secret-tool: cannot store the pi-sh sessions passphrase (is a Secret Service such as GNOME Keyring running?)")
	}
	return nil
}

func (k secretToolKeyring) Delete() error {
	ctx, cancel := keyringContext()
	defer cancel()
	r, err := k.run(ctx, k.tool, append([]string{"clear"}, secretAttrs...), nil)
	if err != nil {
		return errors.New("secret-tool: " + err.Error())
	}
	if r.ExitCode != 0 {
		return errors.New("secret-tool: cannot delete the pi-sh sessions passphrase")
	}
	return nil
}
