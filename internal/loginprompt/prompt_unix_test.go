//go:build darwin || linux

package loginprompt

import (
	"bytes"
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/term"
	"pi-sh/internal/webauth"
)

type syntheticAuth struct {
	passwords int
	codes     int
}

func (*syntheticAuth) Begin(context.Context) (webauth.Stage, error) {
	return webauth.PasswordRequired, nil
}
func (a *syntheticAuth) Password(_ context.Context, email string, password []byte) (webauth.Stage, error) {
	a.passwords++
	if email != "fixture@example.invalid" || string(password) != "synthetic-password" {
		return "", errors.New("synthetic password mismatch")
	}
	return webauth.CodeRequired, nil
}
func (a *syntheticAuth) Code(_ context.Context, code []byte) (webauth.Stage, error) {
	a.codes++
	if string(code) != "123456" {
		return "", errors.New("synthetic code mismatch")
	}
	return webauth.Authenticated, nil
}

func TestCredentialPromptsAndRestoration(t *testing.T) {
	for _, mode := range []string{"complete", "cancel", "account"} {
		cancelled := mode == "cancel"
		// A preselected account (email::device, -l, User or the local
		// default) replaces the email prompt; secrets are still prompted.
		account := map[bool]string{true: "fixture@example.invalid"}[mode == "account"]
		t.Run(mode, func(t *testing.T) {
			master, slave, err := pty.Open()
			if err != nil {
				t.Fatal(err)
			}
			defer master.Close()
			defer slave.Close()
			before, err := term.GetState(int(slave.Fd()))
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			frames := make(chan []byte, 32)
			go func() {
				for {
					b := make([]byte, 1024)
					n, e := master.Read(b)
					if n > 0 {
						select {
						case frames <- b[:n]:
						case <-ctx.Done():
							return
						}
					}
					if e != nil {
						return
					}
				}
			}()
			var captured []byte
			until := func(label string) {
				t.Helper()
				for !bytes.Contains(captured, []byte(label)) {
					select {
					case b := <-frames:
						captured = append(captured, b...)
					case <-ctx.Done():
						t.Fatalf("prompt missing: %s", label)
					}
				}
			}
			a := &syntheticAuth{}
			done := make(chan error, 1)
			go func() { done <- authenticate(ctx, a, slave, slave, account) }()
			if account == "" {
				until("Raspberry Pi ID email")
				if _, err = master.Write([]byte("fixture@example.invalid\n")); err != nil {
					t.Fatal(err)
				}
			} else {
				until("Raspberry Pi ID account: fixture@example.invalid")
			}
			until("Password (hidden")
			if account != "" && bytes.Contains(captured, []byte("Raspberry Pi ID email")) {
				t.Fatal("email prompt shown despite a preselected account")
			}
			for {
				current, _ := term.GetState(int(slave.Fd()))
				if !reflect.DeepEqual(current, before) {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("hidden mode missing")
				default:
					time.Sleep(time.Millisecond)
				}
			}
			if cancelled {
				cancel()
				select {
				case err = <-done:
					if !errors.Is(err, context.Canceled) {
						t.Fatalf("cancel: %v", err)
					}
				case <-time.After(time.Second):
					t.Fatal("cancel did not return")
				}
				after, _ := term.GetState(int(slave.Fd()))
				if !reflect.DeepEqual(before, after) {
					t.Fatal("cancel did not restore terminal")
				}
				// Release the cancelled reader without sending credentials.
				_, _ = master.Write([]byte("\n"))
				return
			}
			if _, err = master.Write([]byte("synthetic-password\n")); err != nil {
				t.Fatal(err)
			}
			until("One-time code (hidden")
			for {
				current, _ := term.GetState(int(slave.Fd()))
				if !reflect.DeepEqual(current, before) {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal("hidden code mode missing")
				default:
					time.Sleep(time.Millisecond)
				}
			}
			if _, err = master.Write([]byte("123456\n")); err != nil {
				t.Fatal(err)
			}
			select {
			case err = <-done:
				if err != nil {
					t.Fatal(err)
				}
			case <-ctx.Done():
				t.Fatal("credential prompt timeout")
			}
			after, _ := term.GetState(int(slave.Fd()))
			if !reflect.DeepEqual(before, after) {
				t.Fatal("success did not restore terminal")
			}
			if a.passwords != 1 || a.codes != 1 {
				t.Fatal("unexpected credential submission count")
			}
			if bytes.Contains(captured, []byte("synthetic-password")) {
				t.Fatal("password was echoed")
			}
		})
	}
}
