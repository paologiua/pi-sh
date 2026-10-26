//go:build darwin || linux

package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/unix"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

func commandPeerPair(t *testing.T, ctx context.Context) (*transport.Channel, *transport.Channel) {
	t.Helper()
	client, err := transport.New(ctx, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	server, err := transport.New(ctx, transport.Config{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { server.Close() })
	ch, err := client.CreateChannel("shell")
	if err != nil {
		t.Fatal(err)
	}
	offer, err := client.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := server.Answer(ctx, offer)
	if err != nil {
		t.Fatal(err)
	}
	if err := client.ApplyAnswer(answer); err != nil {
		t.Fatal(err)
	}
	remote, err := server.Accept(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return ch, remote
}

// This fixture speaks the actual client record protocol but never evaluates
// uploaded source. It is not a Connect/agent simulator or Bash execution proof.
type syntheticFileBroker struct {
	data     []byte
	pressure bool
	input    []byte
	aborted  bool
}

func (s *syntheticFileBroker) serve(ctx context.Context, ch *transport.Channel) error {
	prefix := ""
	phase := 0
	offset, sequence := 0, 0
	stderrDone := false
	send := func(records string) error { return ch.Send(ctx, transport.Message{Text: true, Data: []byte(records)}) }
	var next func() error
	next = func() error {
		if offset < len(s.data) {
			end := min(offset+128, len(s.data))
			sequence++
			record := prefix + "O " + strconv.Itoa(sequence) + " " + hex.EncodeToString(s.data[offset:end]) + "\n"
			offset = end
			return send(record)
		}
		if !stderrDone {
			stderrDone = true
			return send(prefix + "E 1 00ff0d0a\n")
		}
		return send(prefix + "F O\n" + prefix + "F E\n" + prefix + "X 23\n")
	}
	for {
		m, err := ch.Receive(ctx)
		if err != nil {
			return err
		}
		if len(m.Data) == 0 {
			continue
		}
		text := string(m.Data)
		if phase == 0 {
			match := regexp.MustCompile(`PISH3:([a-f0-9]{32}):`).FindStringSubmatch(text)
			if len(match) != 2 {
				return errors.New("missing synthetic marker")
			}
			prefix = "\x1ePISH3:" + match[1] + ":"
			phase = 1
			if err := send(prefix + "P\n"); err != nil {
				return err
			}
			continue
		}
		if phase == 1 {
			phase = 2
			if err := send(prefix + "B\n"); err != nil {
				return err
			}
			continue
		}
		f := strings.Fields(strings.TrimSuffix(strings.TrimPrefix(text, prefix), "\n"))
		if len(f) == 0 {
			return errors.New("empty synthetic record")
		}
		switch f[0] {
		case "L":
			if err := send(prefix + "A L " + f[1] + "\n"); err != nil {
				return err
			}
		case "C":
			if len(f) == 3 {
				if err := send(prefix + "A C " + f[1] + "\n"); err != nil {
					return err
				}
			} else {
				s.aborted = true
				return send(prefix + "D\n")
			}
		case "G":
			if err := send(prefix + "R\n"); err != nil {
				return err
			}
			if s.pressure {
				if err := next(); err != nil {
					return err
				}
			}
		case "I":
			// "I seq length hex": length counts hex digits.
			if len(f) != 4 || f[2] != strconv.Itoa(len(f[3])) {
				return errors.New("malformed synthetic input record")
			}
			part, err := hex.DecodeString(f[3])
			if err != nil {
				return err
			}
			s.input = append(s.input, part...)
			if len(s.input) > 8192 {
				return errors.New("synthetic input limit exceeded")
			}
			if err := send(prefix + "A " + f[1] + "\n"); err != nil {
				return err
			}
		case "F":
			if !s.pressure {
				if err := next(); err != nil {
					return err
				}
			}
		case "A":
			if err := next(); err != nil {
				return err
			}
			if offset == len(s.data) && stderrDone && f[1] == "E" {
				return nil
			}
		default:
			return errors.New("unexpected synthetic broker record")
		}
	}
}

func TestBinaryFileCommandIOAndAcknowledgedCancellation(t *testing.T) {
	for _, name := range []string{"binary-eof", "authorization-backpressure", "cancel-backpressure"} {
		t.Run(name, func(t *testing.T) {
			lifetime, stop := context.WithTimeout(context.Background(), 15*time.Second)
			defer stop()
			ctx, cancel := context.WithCancel(lifetime)
			defer cancel()
			ch, remote := commandPeerPair(t, lifetime)
			in, inWriter, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer inWriter.Close()
			defer in.Close()
			outReader, out, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer out.Close()
			defer outReader.Close()
			errReader, stderr, err := os.Pipe()
			if err != nil {
				t.Fatal(err)
			}
			defer stderr.Close()
			defer errReader.Close()
			outFD := int(out.Fd())
			data := make([]byte, 4096)
			for i := range data {
				data[i] = byte(i)
			}
			pressure := name != "binary-eof"
			if pressure {
				data = bytes.Repeat(data, 256)
			} else {
				if _, err := inWriter.Write(data); err != nil {
					t.Fatal(err)
				}
				inWriter.Close()
			}
			fixture := &syntheticFileBroker{data: data, pressure: pressure}
			peerDone := make(chan error, 1)
			go func() { peerDone <- fixture.serve(lifetime, remote) }()
			plan, err := shellbroker.New("true")
			if err != nil {
				t.Fatal(err)
			}
			failures := make(chan string, 1)
			done := make(chan error, 1)
			go func() { done <- runShellFileCommand(ctx, failures, ch, plan, in, out, stderr) }()
			var stdout, stderrBytes bytes.Buffer
			var drained chan error
			if !pressure {
				drained = make(chan error, 2)
				go func() { _, err := io.Copy(&stdout, outReader); drained <- err }()
				go func() { _, err := io.Copy(&stderrBytes, errReader); drained <- err }()
			} else {
				waitIOReady(t, outFD, unix.POLLOUT, false)
				if name == "cancel-backpressure" {
					cancel()
				} else {
					failures <- "synthetic authorization expired"
				}
			}
			select {
			case err = <-done:
			case <-lifetime.Done():
				t.Fatal("file command did not stop")
			}
			select {
			case peerErr := <-peerDone:
				if peerErr != nil {
					t.Fatal(peerErr)
				}
			case <-lifetime.Done():
				t.Fatal("synthetic peer did not complete")
			}
			if closeErr := out.Close(); closeErr != nil {
				t.Fatal("caller stdout was closed", closeErr)
			}
			if closeErr := stderr.Close(); closeErr != nil {
				t.Fatal("caller stderr was closed", closeErr)
			}
			if !pressure {
				for i := 0; i < 2; i++ {
					if err := <-drained; err != nil {
						t.Fatal(err)
					}
				}
				var status *commandStatus
				if !errors.As(err, &status) || status.code != 23 || !bytes.Equal(stdout.Bytes(), data) || !bytes.Equal(fixture.input, data) || !bytes.Equal(stderrBytes.Bytes(), []byte{0, 255, 13, 10}) {
					t.Fatal("binary stream/status changed", err, stdout.Len(), stderrBytes.Len())
				}
			} else {
				if err == nil || errors.Is(err, shellbroker.ErrCleanupUnconfirmed) || !fixture.aborted {
					t.Fatal("abnormal cleanup not acknowledged", err, fixture.aborted)
				}
				if name == "cancel-backpressure" && !errors.Is(err, context.Canceled) {
					t.Fatal("cancellation lost", err)
				}
				if name == "authorization-backpressure" && !strings.Contains(err.Error(), "synthetic authorization expired") {
					t.Fatal("authorization cause lost", err)
				}
				got, e := io.ReadAll(outReader)
				if e != nil || len(got) == 0 || len(got) >= len(data) || !bytes.Equal(got, data[:len(got)]) {
					t.Fatal("partial stdout corrupted", len(got), e)
				}
				if _, err := inWriter.Write([]byte("next")); err != nil {
					t.Fatal("caller stdin closed", err)
				}
				b := make([]byte, 4)
				if _, err := io.ReadFull(in, b); err != nil || string(b) != "next" {
					t.Fatal("old binary worker consumed future input", string(b), err)
				}
			}
		})
	}
}
