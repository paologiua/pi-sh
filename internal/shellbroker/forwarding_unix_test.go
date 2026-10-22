//go:build darwin || linux

package shellbroker

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestLocalNetcatHalfCloseCandidate(t *testing.T) {
	nc, err := exec.LookPath("nc")
	if err != nil {
		t.Skip("local nc unavailable; remote availability is not inferred")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	listener.SetDeadline(time.Now().Add(3 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, nc, "127.0.0.1", strconv.Itoa(listener.Addr().(*net.TCPAddr).Port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	data := make([]byte, 256*8)
	for i := range data {
		data[i] = byte(i)
	}
	cmd.Stdin = bytes.NewReader(data)
	var output bytes.Buffer
	cmd.Stdout = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("local nc cleanup did not finish")
		}
	}()
	connection, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetReadDeadline(time.Now().Add(time.Second))
	got, err := io.ReadAll(connection)
	if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Skip("this local nc does not supply input EOF by default; dialect-specific flags need separate evidence")
	}
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("nc changed binary request or lost EOF", err)
	}
	if _, err := connection.Write(data); err != nil {
		t.Fatal(err)
	}
	connection.Close()
	select {
	case err := <-done:
		done <- err
		if err != nil {
			t.Fatal("nc failed after half-close", err)
		}
	case <-ctx.Done():
		t.Fatal("nc failed to finish after response")
	}
	if !bytes.Equal(output.Bytes(), data) {
		t.Fatal("nc changed binary response")
	}
}

func TestNativeTCPHalfCloseKeepsReturnStream(t *testing.T) {
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	listener.SetDeadline(time.Now().Add(2 * time.Second))
	client, err := net.DialTCP("tcp4", nil, listener.Addr().(*net.TCPAddr))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	client.SetDeadline(time.Now().Add(2 * time.Second))
	if _, err := client.Write([]byte("request")); err != nil {
		t.Fatal(err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	server, err := listener.AcceptTCP()
	if err != nil {
		t.Fatal(err)
	}
	defer server.Close()
	server.SetDeadline(time.Now().Add(2 * time.Second))
	request, err := io.ReadAll(server)
	if err != nil || string(request) != "request" {
		t.Fatal("EOF-driven request did not finish", err)
	}
	if _, err := server.Write([]byte("response-after-input-eof")); err != nil {
		t.Fatal(err)
	}
	server.Close()
	response, err := io.ReadAll(client)
	if err != nil || string(response) != "response-after-input-eof" {
		t.Fatal("half-close lost the response", err)
	}
}

// This is evidence about a candidate bridge, not an implemented forwarder.
// Closing a Bash fd cannot shut down just the write half of a socket while a
// reader retains another fd for the same socket. EOF-driven peers would hang.
func TestBashTCPBridgeCannotHalfCloseWithReaderAlive(t *testing.T) {
	bash := os.Getenv("PI_SH_TEST_BASH")
	if bash == "" {
		bash, _ = exec.LookPath("bash")
	}
	if bash == "" {
		t.Skip("local Bash unavailable")
	}
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	listener.SetDeadline(time.Now().Add(3 * time.Second))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	port := listener.Addr().(*net.TCPAddr).Port
	cmd := exec.CommandContext(ctx, bash, "--noprofile", "--norc", "-c", fmt.Sprintf(`exec 3<>/dev/tcp/127.0.0.1/%d || exit 78
cat <&3 &
reader=$!
cat >&3
exec 3>&-
wait "$reader"`, port))
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second
	data := make([]byte, 256*8)
	for i := range data {
		data[i] = byte(i)
	}
	cmd.Stdin = bytes.NewReader(data)
	var output, diagnostic bytes.Buffer
	cmd.Stdout, cmd.Stderr = &output, &diagnostic
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("local bridge cleanup did not finish")
		}
	}()
	connection, err := listener.AcceptTCP()
	if err != nil {
		select {
		case err := <-done:
			done <- err
			if exit, ok := err.(*exec.ExitError); ok && exit.ExitCode() == 78 {
				t.Skip("local Bash lacks working /dev/tcp support")
			}
		default:
		}
		t.Fatal(err)
	}
	defer connection.Close()
	connection.SetReadDeadline(time.Now().Add(2 * time.Second))
	got := make([]byte, len(data))
	if _, err := io.ReadFull(connection, got); err != nil || !bytes.Equal(got, data) {
		t.Fatal("candidate bridge changed bytes", err)
	}
	// stdin is already EOF. The socket's write half nevertheless remains open
	// because cat's reader still holds it; require a timeout rather than FIN.
	connection.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	var next [1]byte
	_, err = connection.Read(next[:])
	if timeout, ok := err.(net.Error); !ok || !timeout.Timeout() {
		t.Fatalf("expected missing EOF, got %v", err)
	}
	if _, err := connection.Write([]byte("response-without-input-eof")); err != nil {
		t.Fatal(err)
	}
	connection.Close()
	select {
	case err := <-done:
		done <- err // leave the single Wait result for the cleanup defer
		if err != nil {
			t.Fatalf("bridge exit: %v %s", err, diagnostic.String())
		}
	case <-ctx.Done():
		t.Fatal("bridge failed to finish after peer close")
	}
	if output.String() != "response-without-input-eof" {
		t.Fatal("return stream changed")
	}
}
