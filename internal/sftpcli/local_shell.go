package sftpcli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// singleByteCommands prevents the console reader from taking a shell child's
// future keyboard input into a private read-ahead buffer.
type singleByteCommands struct{ io.Reader }

func (r singleByteCommands) Read(p []byte) (int, error) {
	if len(p) > 1 {
		p = p[:1]
	}
	return r.Reader.Read(p)
}

func (c *Console) localShell(ctx context.Context, command string) error {
	shell := os.Getenv("SHELL")
	windows := false
	if shell == "" {
		shell = "/bin/sh"
		if runtime.GOOS == "windows" {
			shell = os.Getenv("COMSPEC")
			if shell == "" {
				shell = "cmd.exe"
			}
			windows = true
		}
	}
	var args []string
	if command != "" {
		if c.LocalUmask != nil && !windows {
			command = fmt.Sprintf("umask %03o; ", *c.LocalUmask) + command
		}
		flag := "-c"
		if windows {
			flag = "/c"
		}
		args = []string{flag, command}
	} else if c.LocalUmask != nil && !windows {
		// Keep the interactive shell in this process after applying the local mask.
		args = []string{"-c", fmt.Sprintf("umask %03o; exec \"$0\"", *c.LocalUmask), shell}
	}
	child := exec.CommandContext(ctx, shell, args...)
	child.Dir = c.LocalDir
	child.Stdin = c.LocalInput
	if child.Stdin == nil {
		child.Stdin = os.Stdin
	}
	child.Stdout, child.Stderr = c.Output, c.Errors
	err := child.Run()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err == nil {
		return nil
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		if c.Errors != nil {
			if exit.ExitCode() < 0 {
				fmt.Fprint(c.Errors, "Shell exited abnormally\r\n")
			} else {
				fmt.Fprintf(c.Errors, "Shell exited with status %d\r\n", exit.ExitCode())
			}
		}
		return nil // OpenSSH reports the child failure but continues the batch.
	}
	return shown(err, "Couldn't execute %s", display(shell))
}

// A leading ! is passed verbatim to the explicitly requested local shell.
func shellLine(line string) (string, bool) {
	trimmed := strings.TrimLeft(line, " \t")
	if strings.HasPrefix(trimmed, "!") {
		return trimmed[1:], true
	}
	return "", false
}
