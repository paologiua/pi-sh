package shellbroker

import (
	"bytes"
	"context"
	"io"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

type fragmentStream struct{ chunks [][]byte }

func (s *fragmentStream) Send(context.Context, []byte) error { return nil }
func (s *fragmentStream) Receive(context.Context) ([]byte, error) {
	if len(s.chunks) == 0 {
		return nil, io.EOF
	}
	b := s.chunks[0]
	s.chunks = s.chunks[1:]
	return b, nil
}

func TestWireFragmentationAndBounds(t *testing.T) {
	p, err := New("true")
	if err != nil {
		t.Fatal(err)
	}
	data := []byte("prompt and echo\r\n" + p.prefix + "B\n" + p.prefix + "O 1 00ff0d0a\n")
	for size := 1; size <= len(data); size++ {
		stream := &fragmentStream{}
		for start := 0; start < len(data); start += size {
			stream.chunks = append(stream.chunks, data[start:min(start+size, len(data))])
		}
		w := &wire{ctx: context.Background(), stream: stream, prefix: p.prefix}
		first, err := w.next()
		if err != nil || first != "B" {
			t.Fatalf("size %d first=%q err=%v", size, first, err)
		}
		second, err := w.next()
		if err != nil || second != "O 1 00ff0d0a" {
			t.Fatalf("size %d second=%q err=%v", size, second, err)
		}
	}
	for _, data := range []string{strings.Repeat("x", 70*1024), p.prefix + strings.Repeat("x", maxRecord+1) + "\n", p.prefix + "B\ninvalid\n"} {
		w := &wire{ctx: context.Background(), stream: &fragmentStream{chunks: [][]byte{[]byte(data)}}, prefix: p.prefix}
		first, err := w.next()
		if err == nil && first == "B" {
			_, err = w.next()
		}
		if err == nil {
			t.Fatal("malformed or oversized record accepted")
		}
	}
}

func TestBootstrapHasNoRemoteFilesOrPython(t *testing.T) {
	p, err := New("printf 'a\\n';\nprintf 'é'")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.bootstrap) > 512 || bytes.ContainsAny([]byte(p.bootstrap), "\n") {
		t.Fatal("initial bootstrap is not a short single line")
	}
	for _, code := range []string{p.bootstrap, p.loader, source} {
		for _, bad := range []string{"<<<", "<<EOF", "mkfifo", "mktemp", "python3", "/tmp/", ">(", "<("} {
			if strings.Contains(code, bad) {
				t.Fatalf("unexpected filesystem/runtime construct %q", bad)
			}
		}
	}
	for _, bad := range []string{"bad\x00command", string([]byte{255}), strings.Repeat("x", 128*1024+1)} {
		if _, err := New(bad); err == nil {
			t.Fatal("invalid command accepted")
		}
	}
}

// The broker script and the client must agree on the record window, or the
// broker fails the protocol when the client sends more input records.
func TestBrokerWindowMatchesClient(t *testing.T) {
	m := regexp.MustCompile(`(?m)^_ps_win=([0-9]+)$`).FindStringSubmatch(source)
	if m == nil {
		t.Fatal("_ps_win not found in broker.bash")
	}
	if n, _ := strconv.Atoi(m[1]); n != recordWindow {
		t.Fatalf("_ps_win=%d, recordWindow=%d", n, recordWindow)
	}
}
