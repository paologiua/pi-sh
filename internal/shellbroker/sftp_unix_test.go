//go:build darwin || linux

package shellbroker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func TestSFTPOverActualBrokerAndOpenSSHServer(t *testing.T) {
	testSFTPOverActualBroker(t, false)
}

func TestSFTPMegabyteOverActualBrokerWebRTCAndOpenSSHServer(t *testing.T) {
	testSFTPOverActualBroker(t, true)
}

func testSFTPOverActualBroker(t *testing.T, overWebRTC bool) {
	bash := os.Getenv("PI_SH_TEST_BASH")
	if bash == "" {
		bash, _ = exec.LookPath("bash")
	}
	if bash == "" || exec.Command(bash, "-c", "((BASH_VERSINFO[0]>4||(BASH_VERSINFO[0]==4&&BASH_VERSINFO[1]>=3)))").Run() != nil {
		t.Skip("Bash >=4.3 is required for the local broker fixture")
	}
	server := ""
	for _, candidate := range []string{"/usr/libexec/sftp-server", "/usr/lib/openssh/sftp-server", "/usr/libexec/openssh/sftp-server"} {
		if info, err := os.Stat(candidate); err == nil && info.Mode().IsRegular() {
			server = candidate
			break
		}
	}
	if server == "" {
		t.Skip("local OpenSSH sftp-server is not installed")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 95*time.Second)
	defer cancel()
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	defer inR.Close()
	defer inW.Close()
	defer outR.Close()
	defer outW.Close()
	type outcome struct {
		code int
		err  error
	}
	done := make(chan outcome, 1)
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		// A fatal assertion in the PTY harness exits this goroutine too.
		// Always release the client instead of leaving a READ blocked forever.
		defer outW.Close()
		defer inR.Close()
		factory := func(ctx context.Context, file *os.File) Stream { return newTestStream(ctx, file) }
		if overWebRTC {
			factory = func(ctx context.Context, file *os.File) Stream { return localWebRTCPTY(t, ctx, file) }
		}
		code, err := runLocalWithStream(t, "exec "+quote(server)+" -e", func(context.CancelFunc) IO {
			return IO{Stdin: inR, Stdout: outW, Stderr: io.Discard}
		}, nil, 90*time.Second, factory)
		if code != 0 && err == nil {
			err = fmt.Errorf("local test server status %d", code)
		}
		outW.CloseWithError(err)
		inR.Close()
		done <- outcome{code, err}
	}()
	go func() {
		select {
		case <-ctx.Done():
			inR.CloseWithError(ctx.Err())
			outR.CloseWithError(ctx.Err())
		case <-finished:
		}
	}()
	client, err := sftp.NewClientPipe(outR, inW, sftp.MaxPacketChecked(1024), sftp.MaxConcurrentRequestsPerFile(4))
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	dir := t.TempDir()
	original := filepath.Join(dir, "binary ' 世界")
	renamed := filepath.Join(dir, "renamed")
	// Exercise the same all-byte size that failed on the real Connect carrier,
	// including the broker/PTY rather than only a direct SFTP pipe.
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i)
	}
	file, err := client.Create(original)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = file.Write(data); err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if err = client.Rename(original, renamed); err != nil {
		t.Fatal(err)
	}
	file, err = client.Open(renamed)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(file)
	if err != nil {
		t.Fatal(err)
	}
	if err = file.Close(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("SFTP bytes changed over actual broker/PTY")
	}
	if err = client.Remove(renamed); err != nil {
		t.Fatal(err)
	}
	if err = client.Close(); err != nil {
		t.Fatal(err)
	}
	// done is consumed only here; the guardian uses a separate completion signal.
	select {
	case r := <-done:
		if r.err != nil || r.code != 0 {
			t.Fatalf("broker EOF/status: %d %v", r.code, r.err)
		}
	case <-ctx.Done():
		t.Fatal(errors.New("SFTP/broker EOF did not finish"))
	}
}
