// Package account validates the Raspberry Pi ID account (an email address)
// that plays the role of the SSH remote user, and reads the optional local
// non-secret default. It never stores, writes or transmits anything, and it
// never handles passwords or one-time codes.
package account

import (
	"bufio"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// EnvName selects a default account for direct login. An empty value
// disables the default, including the file below.
const EnvName = "PI_SH_ACCOUNT"

// MaxLength bounds an account. RFC 5321 limits a forward path to 254 bytes.
const MaxLength = 254

// maxFileBytes bounds the default account file.
const maxFileBytes = 4096

var (
	// ErrInvalid is returned for values that are not a plain ASCII email.
	ErrInvalid = errors.New("invalid Raspberry Pi ID account: expected an email address such as name@example.com")
	errFile    = errors.New("default account file (XDG_CONFIG_HOME or ~/.config, pi-sh/account) must be a readable regular file containing one email address")
)

// Validate accepts a conservative ASCII email address: one '@', a nonempty
// local part of printable characters without spaces or address specials, and
// a dotted domain of letters, digits and hyphens. It is a local syntax check;
// Raspberry Pi ID decides whether the account exists.
func Validate(s string) error {
	if len(s) < 3 || len(s) > MaxLength || strings.Count(s, "@") != 1 {
		return ErrInvalid
	}
	local, domain, _ := strings.Cut(s, "@")
	if local == "" || len(local) > 64 || strings.HasPrefix(local, ".") || strings.HasSuffix(local, ".") || strings.Contains(local, "..") {
		return ErrInvalid
	}
	for i := 0; i < len(local); i++ {
		c := local[i]
		if c <= ' ' || c >= 127 || strings.IndexByte("\"(),:;<>[\\]", c) >= 0 {
			return ErrInvalid
		}
	}
	labels := strings.Split(domain, ".")
	if len(labels) < 2 {
		return ErrInvalid
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return ErrInvalid
		}
		for i := 0; i < len(label); i++ {
			c := label[i]
			if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-') {
				return ErrInvalid
			}
		}
	}
	return nil
}

// Default returns the local default account: PI_SH_ACCOUNT when set (empty
// disables any default), otherwise the file pi-sh/account under
// $XDG_CONFIG_HOME (when absolute) or ~/.config. A missing file means no
// default. An invalid value or unreadable file is an error, never ignored.
// The file is only read; it is never created or written.
func Default() (string, error) {
	return defaultFrom(os.LookupEnv, os.UserHomeDir)
}

func defaultFrom(lookup func(string) (string, bool), home func() (string, error)) (string, error) {
	if value, ok := lookup(EnvName); ok {
		if value == "" {
			return "", nil
		}
		if err := Validate(value); err != nil {
			return "", errors.New("PI_SH_ACCOUNT: " + err.Error())
		}
		return value, nil
	}
	path, err := filePath(lookup, home)
	if err != nil || path == "" {
		return "", err
	}
	return readFile(path)
}

// FilePath reports where the default account file is looked up, or "" when
// no home directory is known.
func FilePath() string {
	path, _ := filePath(os.LookupEnv, os.UserHomeDir)
	return path
}

func filePath(lookup func(string) (string, bool), home func() (string, error)) (string, error) {
	// The XDG base directory specification ignores relative values.
	if dir, ok := lookup("XDG_CONFIG_HOME"); ok && filepath.IsAbs(dir) {
		return filepath.Join(dir, "pi-sh", "account"), nil
	}
	dir, err := home()
	if err != nil || dir == "" {
		return "", nil
	}
	return filepath.Join(dir, ".config", "pi-sh", "account"), nil
}

// readFile accepts blank lines and '#' comment lines around exactly one
// account line; surrounding whitespace is removed.
func readFile(path string) (string, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return "", errFile
	}
	f, err := os.Open(path)
	if err != nil {
		return "", errFile
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxFileBytes+1))
	if err != nil || len(data) > maxFileBytes {
		return "", errFile
	}
	var found string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if found != "" {
			return "", errFile
		}
		found = line
	}
	if found == "" {
		return "", errFile
	}
	if err := Validate(found); err != nil {
		return "", errors.New("default account file: " + err.Error())
	}
	return found, nil
}
