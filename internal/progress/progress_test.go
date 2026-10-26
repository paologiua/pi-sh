package progress

import (
	"bytes"
	"io"
	"math"
	"os"
	"strings"
	"testing"
	"time"
)

func TestLayoutSpeedETAStallAndCompletion(t *testing.T) {
	now := time.Unix(100, 0)
	s := snapshot{start: now, last: now, total: 2000}
	prefix := "\r" + "file" + strings.Repeat(" ", 41)
	for _, tc := range []struct {
		count, seconds int64
		want           string
	}{
		{0, 0, "   0%    0     0.0KB/s   --:-- ETA"},
		{1000, 1, "  50% 1000     1.0KB/s   00:01 ETA"},
		{1000, 6, "  50% 1000     0.9KB/s - stalled -"},
		{2000, 7, " 100% 2000     0.3KB/s   00:07    "},
	} {
		if got := s.line("file", tc.count, now.Add(time.Duration(tc.seconds)*time.Second), 80, true); got != prefix+tc.want {
			t.Fatalf("count=%d: got %q want %q", tc.count, got, prefix+tc.want)
		}
	}
}

func TestUnitsLargeOffsetsEmptyAndNarrowTerminal(t *testing.T) {
	for _, tc := range []struct {
		n    int64
		want string
	}{{0, "   0  "}, {9999, "9999  "}, {10000, "  10KB"}, {1 << 30, "1024MB"}, {math.MaxInt64, "8388608TB"}} {
		if got := formatSize(tc.n); got != tc.want {
			t.Errorf("size %d: %q != %q", tc.n, got, tc.want)
		}
	}
	now := time.Unix(1, 0)
	s := snapshot{start: now, last: now, total: 5 << 30, offset: 3 << 30, pos: 3 << 30}
	line := s.line("large", 3<<30, now, 80, true)
	if !strings.Contains(line, " 60% 3072MB") {
		t.Fatal(line)
	}
	for _, columns := range []int{1, 2, 3, 20, 35, 80, 512} {
		s := snapshot{start: now, last: now}
		line := s.line("empty", 0, now, columns, true)
		if len(line) > columns {
			t.Fatalf("line exceeds terminal: %d %q", columns, line)
		}
		if columns == 80 && (!strings.Contains(line, "100%") || strings.Contains(line, "ETA")) {
			t.Fatal(line)
		}
	}
}

func TestNamesAreSafeAndColumnBounded(t *testing.T) {
	for _, tc := range []struct {
		text string
		cols int
		utf8 bool
		want string
	}{
		{"a\x1b[31m\n", 40, true, `a\033[31m\012`},
		{"\xff", 4, true, `\377`},
		{"\x1b", 3, true, ""},
		{"世界-é", 4, true, "世界"},
		{"e\u0301x", 1, true, "e\u0301"},
		{"é", 8, false, `\303\251`},
		{"\u202e", 12, true, `\342\200\256`},
	} {
		if got := fit(tc.text, tc.cols, tc.utf8, false); got != tc.want {
			t.Errorf("%q: %q != %q", tc.text, got, tc.want)
		}
	}
	if got := fit(strings.Repeat("\u0301", 10000), 80, true, true); len(got) > 4096+80 {
		t.Fatal("unbounded filename")
	}
}

func TestPartialTransferStopsOnceWithoutClaimingCompletion(t *testing.T) {
	var out bytes.Buffer
	d := New(&out, func() (int, bool) { return 80, true })
	m := d.Start("partial", 1024, 0)
	m.Add(256)
	m.Stop()
	m.Stop()
	if !strings.Contains(out.String(), " 25%") || strings.Contains(out.String(), "100%") || strings.Count(out.String(), "\n") != 1 {
		t.Fatalf("incorrect partial completion: %q", out.String())
	}
	select {
	case <-m.done:
	default:
		t.Fatal("renderer still running")
	}
	var missing *Display
	missing.Start("disabled", 0, 0).Stop()
}

func TestBackgroundAndRedirectedOutputAreSilent(t *testing.T) {
	var out bytes.Buffer
	m := New(&out, func() (int, bool) { return 80, false }).Start("hidden", 1, 0)
	m.Add(1)
	m.Stop()
	if out.Len() != 0 {
		t.Fatal("background meter wrote output")
	}
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if NewTerminal(w, false) != nil {
		t.Fatal("pipe enables progress")
	}
	w.Close()
	if b, _ := io.ReadAll(r); len(b) != 0 {
		t.Fatal("pipe polluted")
	}
}
