//go:build darwin || linux

package localio

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

func flags(t *testing.T, file *os.File) int {
	t.Helper()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	value := 0
	var getErr error
	if err := raw.Control(func(fd uintptr) { value, getErr = unix.FcntlInt(fd, unix.F_GETFL, 0) }); err != nil {
		t.Fatal(err)
	}
	if getErr != nil {
		t.Fatal(getErr)
	}
	return value
}

func pipe(t *testing.T) (*os.File, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close(); w.Close() })
	return r, w
}

func waitReadable(t *testing.T, file *os.File) {
	t.Helper()
	raw, err := file.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	fd := -1
	if err := raw.Control(func(v uintptr) { fd = int(v) }); err != nil {
		t.Fatal(err)
	}
	fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLIN}}
	n, err := unix.Poll(fds, 1000)
	if err != nil || n == 0 {
		t.Fatal("fixture did not become readable", err)
	}
}

func TestReadCancellationRestoresFlagsAndLeavesFileReusable(t *testing.T) {
	for _, blocking := range []bool{true, false} {
		r, w := pipe(t)
		if blocking {
			_ = r.Fd()
		} // Establish the caller's deliberate blocking baseline.
		before := flags(t, r)
		ctx, cancel := context.WithCancel(context.Background())
		s, err := Open(ctx, Files{Stdin: r})
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { _, err := s.Stdin.Read(make([]byte, 1)); done <- err }()
		cancel()
		select {
		case err := <-done:
			if !errors.Is(err, context.Canceled) {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("read did not cancel")
		}
		var closeWG sync.WaitGroup
		for i := 0; i < 8; i++ {
			closeWG.Add(1)
			go func() {
				defer closeWG.Done()
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			}()
		}
		closeWG.Wait()
		if got := flags(t, r); got != before {
			t.Fatalf("flags %x want %x", got, before)
		}
		if _, err := w.Write([]byte("next")); err != nil {
			t.Fatal(err)
		}
		b := make([]byte, 4)
		if _, err := io.ReadFull(r, b); err != nil || string(b) != "next" {
			t.Fatal("borrowed input not reusable", string(b), err)
		}
		if _, err := s.Stdin.Read(b); !errors.Is(err, context.Canceled) {
			t.Fatal("closed session touched reused input", err)
		}
	}
}

type writeResult struct {
	n   int
	err error
}

func TestCancelWakesInputAndBothBackpressuredOutputs(t *testing.T) {
	in, inWriter := pipe(t)
	outReader, out := pipe(t)
	errReader, stderr := pipe(t)
	_ = in.Fd()
	_ = out.Fd()
	_ = stderr.Fd()
	before := []int{flags(t, in), flags(t, out), flags(t, stderr)}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Open(ctx, Files{Stdin: in, Stdout: out, Stderr: stderr})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i)
	}
	readDone := make(chan error, 1)
	outDone, errDone := make(chan writeResult, 1), make(chan writeResult, 1)
	go func() { _, err := s.Stdin.Read(make([]byte, 8)); readDone <- err }()
	go func() { n, err := s.Stdout.Write(data); outDone <- writeResult{n, err} }()
	go func() { n, err := s.Stderr.Write(data); errDone <- writeResult{n, err} }()
	waitReadable(t, outReader)
	waitReadable(t, errReader)
	select {
	case r := <-outDone:
		t.Fatal("stdout was not backpressured", r)
	default:
	}
	select {
	case r := <-errDone:
		t.Fatal("stderr was not backpressured", r)
	default:
	}
	cancel()
	select {
	case err := <-readDone:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("input did not wake")
	}
	results := make([]writeResult, 0, 2)
	for _, done := range []<-chan writeResult{outDone, errDone} {
		select {
		case r := <-done:
			if !errors.Is(r.err, context.Canceled) || r.n <= 0 || r.n >= len(data) {
				t.Fatal("write cancellation/prefix", r)
			}
			results = append(results, r)
		case <-time.After(time.Second):
			t.Fatal("output did not wake")
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for i, f := range []*os.File{in, out, stderr} {
		mask := unix.O_NONBLOCK | unix.O_APPEND | unix.O_ASYNC | unix.O_SYNC | unix.O_DSYNC | unix.O_ACCMODE
		if got := flags(t, f); got&mask != before[i]&mask {
			t.Fatalf("standard-stream%d flags %x want %x", i, got, before[i])
		}
	}
	for i, r := range []*os.File{outReader, errReader} {
		got := make([]byte, results[i].n)
		if _, err := io.ReadFull(r, got); err != nil || !bytes.Equal(got, data[:len(got)]) {
			t.Fatal("partial output corrupted", err)
		}
		writer := []*os.File{out, stderr}[i]
		if _, err := writer.Write([]byte("ok")); err != nil {
			t.Fatal("output file closed", err)
		}
		ack := make([]byte, 2)
		if _, err := io.ReadFull(r, ack); err != nil || string(ack) != "ok" {
			t.Fatal("output file not reusable", err)
		}
	}
	if _, err := inWriter.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
}

func TestAliasedTTYDescriptorsRestoreOriginalFlagsTogether(t *testing.T) {
	m, file, err := pty.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	defer file.Close()
	fd := int(file.Fd())
	dup := func() *os.File {
		n, err := unix.Dup(fd)
		if err != nil {
			t.Fatal(err)
		}
		f := os.NewFile(uintptr(n), "local-tty-alias")
		t.Cleanup(func() { f.Close() })
		return f
	}
	output, stderr := dup(), dup()
	before := flags(t, file)
	s, err := Open(context.Background(), Files{Stdin: file, Stdout: output, Stderr: stderr})
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []*os.File{file, output, stderr} {
		if flags(t, f)&unix.O_NONBLOCK == 0 {
			t.Fatal("nonblocking ownership not acquired")
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	for _, f := range []*os.File{file, output, stderr} {
		if flags(t, f) != before {
			t.Fatal("alias retained another wrapper's temporary flags")
		}
	}
}

func TestRegularFileBinaryAppendAndSetupRefusal(t *testing.T) {
	f, err := os.OpenFile(filepath.Join(t.TempDir(), "content"), os.O_RDWR|os.O_CREATE|os.O_APPEND, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	_ = f.Fd()
	if _, err := f.Write([]byte("prefix")); err != nil {
		t.Fatal(err)
	}
	before := flags(t, f)
	s, err := Open(context.Background(), Files{Stdout: f})
	if err != nil {
		t.Fatal(err)
	}
	data := make([]byte, 256)
	for i := range data {
		data[i] = byte(i)
	}
	if n, err := s.Stdout.Write(data); err != nil || n != len(data) {
		t.Fatal(n, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if flags(t, f) != before {
		t.Fatal("append/status flags changed")
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(f)
	if err != nil || !bytes.Equal(got, append([]byte("prefix"), data...)) {
		t.Fatal("regular output corrupted", err)
	}
	r, closed := pipe(t)
	closed.Close()
	initial := flags(t, r)
	if _, err := Open(context.Background(), Files{Stdin: r, Stdout: closed}); !errors.Is(err, ErrSetup) {
		t.Fatal("closed source accepted", err)
	}
	if flags(t, r) != initial {
		t.Fatal("failed setup changed earlier descriptor flags")
	}
}

func TestSuspendedIOHandsFlagsBackAndCapturesNewBaseline(t *testing.T) {
	r, w := pipe(t)
	raw, err := w.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	var setErr error
	if err := raw.Control(func(fd uintptr) { setErr = unix.SetNonblock(int(fd), false) }); err != nil || setErr != nil {
		t.Fatal(err, setErr)
	}
	before := flags(t, w)
	s, err := Open(context.Background(), Files{Stdout: w})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	called := false
	err = s.WithSuspended(func() error {
		called = true
		if got := flags(t, w); got != before {
			t.Fatalf("suspended flags %x want %x", got, before)
		}
		if _, err := w.Write([]byte("local-shell")); err != nil {
			return err
		}
		return raw.Control(func(fd uintptr) { setErr = unix.SetNonblock(int(fd), true) })
	})
	if err != nil || setErr != nil || !called {
		t.Fatal("suspend action failed", err, setErr)
	}
	if _, err := s.Stdout.Write([]byte("resumed")); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if flags(t, w)&unix.O_NONBLOCK == 0 {
		t.Fatal("resume discarded shell's changed baseline")
	}
	b := make([]byte, len("local-shellresumed"))
	if _, err := io.ReadFull(r, b); err != nil || string(b) != "local-shellresumed" {
		t.Fatal("handover bytes changed", string(b), err)
	}
}

func TestSuspendDrainCancellationNeverRunsAction(t *testing.T) {
	r, w := pipe(t)
	_ = r
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s, err := Open(ctx, Files{Stdout: w})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	writeDone := make(chan error, 1)
	go func() { _, err := s.Stdout.Write(bytes.Repeat([]byte{1}, 1<<20)); writeDone <- err }()
	// A full pipe establishes an in-flight blocked output operation.
	raw, err := w.SyscallConn()
	if err != nil {
		t.Fatal(err)
	}
	fd := -1
	raw.Control(func(v uintptr) { fd = int(v) })
	deadline := time.Now().Add(time.Second)
	for {
		fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLOUT}}
		if _, err := unix.Poll(fds, 0); err != nil {
			t.Fatal(err)
		}
		if fds[0].Revents&unix.POLLOUT == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fixture output did not fill")
		}
		time.Sleep(time.Millisecond)
	}
	paused := make(chan error, 1)
	go func() { paused <- s.WithSuspended(func() error { return errors.New("action ran after cancellation") }) }()
	cancel()
	if err := <-writeDone; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := <-paused; !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled drain invoked action", err)
	}
}

// Darwin's poll reports POLLNVAL for /dev/null although read/write work, so
// `cmd < /dev/null` and `> /dev/null` must behave as EOF and a sink.
func TestDevNullStreams(t *testing.T) {
	in, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	s, err := Open(context.Background(), Files{Stdin: in, Stdout: out})
	if err != nil {
		t.Fatal(err)
	}
	if n, err := s.Stdin.Read(make([]byte, 16)); n != 0 || err != io.EOF {
		t.Fatalf("read /dev/null: n=%d err=%v", n, err)
	}
	if n, err := s.Stdout.Write([]byte("discarded")); n != 9 || err != nil {
		t.Fatalf("write /dev/null: n=%d err=%v", n, err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}
