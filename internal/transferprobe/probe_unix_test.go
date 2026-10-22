//go:build darwin || linux

package transferprobe

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

func probeClient(t *testing.T) *sftp.Client {
	t.Helper()
	server := ""
	for _, candidate := range []string{"/usr/libexec/sftp-server", "/usr/lib/openssh/sftp-server", "/usr/libexec/openssh/sftp-server"} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			server = candidate
			break
		}
	}
	if server == "" {
		t.Skip("local OpenSSH sftp-server unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, server, "-u", "077")
	in, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	client, err := sftp.NewClientPipe(out, in, sftp.MaxPacketChecked(1024), sftp.MaxConcurrentRequestsPerFile(4))
	if err != nil {
		in.Close()
		cmd.Wait()
		t.Fatal(err)
	}
	t.Cleanup(func() {
		client.Close()
		in.Close()
		if err := cmd.Wait(); err != nil {
			t.Error(err)
		}
	})
	return client
}

func TestCompleteProbeAgainstLocalOpenSSHAndVerifiedCleanup(t *testing.T) {
	client := probeClient(t)
	root, err := NewRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := Cleanup(client, root); err != nil {
			t.Error("local probe leftovers", err)
		}
	})
	var events []Event
	err = Run(context.Background(), client, root, 0022, func(event Event) { events = append(events, event) })
	if err != nil {
		t.Fatal(err, events)
	}
	for _, event := range events {
		if !event.Passed && !event.Skipped {
			t.Fatal("failed check became success", event)
		}
	}
	if len(events) != 22 || events[0].Check != "private-root" || events[len(events)-1].Check != "cleanup" || !events[len(events)-1].Passed {
		t.Fatal("verification/cleanup events incomplete", events)
	}
	if _, err := os.Lstat(root); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("remote scratch root remains", err)
	}
}

func TestExistingScratchIsNeverAdoptedOrRemoved(t *testing.T) {
	client := probeClient(t)
	root, err := NewRoot()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(root + "/keep"); os.Remove(root) })
	if err := os.WriteFile(root+"/keep", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	err = Run(context.Background(), client, root, 0022, nil)
	if err == nil {
		t.Fatal("existing root adopted")
	}
	got, err := os.ReadFile(root + "/keep")
	if err != nil || !bytes.Equal(got, []byte("keep")) {
		t.Fatal("existing scratch data changed", err)
	}
}

func TestCleanupRefusesUnknownNamesAndSymlinksBeforeDeletion(t *testing.T) {
	client := probeClient(t)
	for _, kind := range []string{"unknown", "symlink"} {
		t.Run(kind, func(t *testing.T) {
			root, err := NewRoot()
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(root, 0700); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { os.Remove(root + "/unknown"); os.Remove(root + "/binary"); os.Remove(root) })
			if kind == "unknown" {
				if err := os.WriteFile(root+"/binary", []byte("known"), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(root+"/unknown", []byte("keep"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				if err := os.Symlink("/does-not-exist", root+"/binary"); err != nil {
					t.Fatal(err)
				}
			}
			if err := Cleanup(client, root); err == nil {
				t.Fatal("unrecognized scratch inventory was deleted")
			}
			if _, err := os.Lstat(root + "/binary"); err != nil {
				t.Fatal("cleanup deleted known entry before refusing", err)
			}
		})
	}
}

func TestProbeLostTransportCannotClaimCleanup(t *testing.T) {
	client := probeClient(t)
	root, err := NewRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(root + "/empty"); os.Remove(root) })
	cleanupReported, cleanupPassed, uploadFailed := false, false, false
	err = Run(context.Background(), client, root, 0022, func(event Event) {
		if event.Check == "empty-roundtrip" && event.Passed {
			client.Close()
		}
		if event.Check == "cleanup" {
			cleanupReported, cleanupPassed = true, event.Passed
		}
		if event.Check == "binary-upload" {
			uploadFailed = !event.Passed
		}
	})
	if err == nil || !cleanupReported || cleanupPassed || !uploadFailed {
		t.Fatal("lost connection claimed cleanup", err)
	}
	if _, err := os.Lstat(root); err != nil {
		t.Fatal("fixture did not preserve actual cleanup uncertainty", err)
	}
}

func TestProbeCancellationAndUnexpectedEntryRetainFailure(t *testing.T) {
	client := probeClient(t)
	root, err := NewRoot()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Remove(root + "/unknown"); os.Remove(root + "/empty"); os.Remove(root) })
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cleanupReported, cleanupPassed := false, false
	err = Run(ctx, client, root, 0022, func(event Event) {
		if event.Check == "empty-roundtrip" && event.Passed {
			if err := os.WriteFile(root+"/unknown", []byte("keep"), 0600); err != nil {
				t.Fatal(err)
			}
			cancel()
		}
		if event.Check == "cleanup" {
			cleanupReported, cleanupPassed = true, event.Passed
		}
	})
	if !errors.Is(err, context.Canceled) || !cleanupReported || cleanupPassed {
		t.Fatal("cancellation/unknown entry became cleanup success", err)
	}
	for _, name := range []string{"empty", "unknown"} {
		if _, err := os.Lstat(root + "/" + name); err != nil {
			t.Fatal("refused cleanup deleted an entry", err)
		}
	}
}
