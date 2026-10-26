// Package progress displays per-file transfer progress using scp's terminal
// layout and timing rules. It never owns terminal modes or the transfer stream.
package progress

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/term"
	"golang.org/x/text/width"
)

// Display is reusable for sequential files. Terminal returns current columns
// and whether output is in the foreground; it must be safe during a transfer.
type Display struct {
	output   io.Writer
	terminal func() (int, bool)
	utf8     bool
}

// New separates terminal inspection from formatting, also allowing embedders
// and deterministic tests to supply their own terminal surface.
func New(output io.Writer, terminal func() (int, bool)) *Display {
	if output == nil || terminal == nil {
		return nil
	}
	return &Display{output: output, terminal: terminal, utf8: utf8Locale()}
}

// NewTerminal follows scp: progress goes to stdout only when it is a terminal.
// Quiet is independent of connection LogLevel and remains sticky after -v.
func NewTerminal(output *os.File, quiet bool) *Display {
	if quiet || output == nil {
		return nil
	}
	raw, err := output.SyscallConn()
	if err != nil {
		return nil
	}
	isTerminal := false
	if err := raw.Control(func(fd uintptr) { isTerminal = term.IsTerminal(int(fd)) }); err != nil || !isTerminal {
		return nil
	}
	return New(output, func() (int, bool) {
		cols, canOutput := 80, false
		err := raw.Control(func(fd uintptr) {
			if width, _, err := term.GetSize(int(fd)); err == nil && width > 0 {
				cols = width
			}
			canOutput = foreground(int(fd))
		})
		return cols, err == nil && canOutput
	})
}

// Transfer counts successfully written data; failed writes never imply success.
// Add is safe concurrently with periodic rendering and Stop. Stop joins the
// renderer and ends the line, without closing the caller's output.
type Transfer struct {
	display     *Display
	name        string
	count       atomic.Int64
	state       snapshot
	stop, done  chan struct{}
	once        sync.Once
	writeFailed bool
}

func (d *Display) Start(name string, total, offset int64) *Transfer {
	if d == nil {
		return nil
	}
	now := time.Now()
	m := &Transfer{display: d, name: name, stop: make(chan struct{}), done: make(chan struct{}),
		state: snapshot{start: now, last: now, total: max(total, 0), offset: max(offset, 0), pos: max(offset, 0)}}
	m.count.Store(max(offset, 0))
	m.refresh(now)
	go func() {
		defer close(m.done)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		resize, stopResize := resizeEvents()
		defer stopResize()
		for {
			select {
			case <-m.stop:
				return
			case <-ticker.C:
				m.refresh(time.Now())
			case <-resize:
				m.refresh(time.Now())
			}
		}
	}()
	return m
}

func (m *Transfer) Add(n int) {
	if m != nil && n > 0 {
		m.count.Add(int64(n))
	}
}

func (m *Transfer) Stop() {
	if m == nil {
		return
	}
	m.once.Do(func() {
		close(m.stop)
		<-m.done
		if m.state.pos != m.state.total {
			m.refresh(time.Now())
		}
		_, foreground := m.display.terminal()
		if foreground && !m.writeFailed {
			_, _ = io.WriteString(m.display.output, "\n")
		}
	})
}

func (m *Transfer) refresh(now time.Time) {
	cols, foreground := m.display.terminal()
	if !foreground || m.writeFailed {
		return
	}
	if cols <= 0 {
		cols = 80
	}
	cols = min(cols, 512)
	line := m.state.line(m.name, m.count.Load(), now, cols, m.display.utf8)
	if line != "" {
		n, err := io.WriteString(m.display.output, line)
		m.writeFailed = err != nil || n != len(line)
	}
}

type snapshot struct {
	start, last              time.Time
	total, offset, pos, rate int64
	stalled                  time.Duration
}

func (s *snapshot) line(name string, count int64, now time.Time, cols int, utf8Mode bool) string {
	delta := count - s.pos
	elapsed := max(now.Sub(s.last), 0)
	s.pos = count
	left := s.total - count
	if left <= 0 {
		elapsed = max(now.Sub(s.start), 0)
		delta = s.total - s.offset
		s.rate = 0 // Completed transfers show their total average speed.
	}
	speed := float64(delta)
	if elapsed > 0 {
		speed /= elapsed.Seconds()
	}
	speed = max(0, min(speed, float64(math.MaxInt64)))
	if s.rate != 0 {
		speed = float64(s.rate)*0.9 + speed*0.1
	}
	if speed >= float64(math.MaxInt64) {
		s.rate = math.MaxInt64
	} else {
		s.rate = int64(speed)
	}
	if delta == 0 {
		s.stalled += elapsed
	} else {
		s.stalled = 0
	}
	s.last = now
	if cols < 3 {
		return ""
	}
	percent := 100
	if s.total != 0 && count != s.total {
		percent = int(float32(count) / float32(s.total) * 100)
	}
	eta := ""
	switch {
	case s.stalled >= 5*time.Second:
		eta = "- stalled -"
	case s.rate == 0 && left > 0:
		eta = "  --:-- ETA"
	default:
		seconds := int64(elapsed.Seconds())
		if left > 0 {
			seconds = left / s.rate
		}
		if seconds >= 3600 {
			eta = fmt.Sprintf("%d:%02d:%02d", seconds/3600, seconds/60%60, seconds%60)
		} else {
			eta = fmt.Sprintf("  %02d:%02d", seconds/60, seconds%60)
		}
		if left > 0 {
			eta += " ETA"
		} else {
			eta += "    "
		}
	}
	name = fit(name, max(cols-35, 0), utf8Mode, true)
	line := fmt.Sprintf("%s %3d%% %s %s/s %s", name, percent, formatSize(count), formatRate(s.rate), eta)
	// scp reserves a column for the leading carriage return, without ANSI codes.
	line = fit(" "+line, cols, utf8Mode, false)
	return "\r" + strings.TrimPrefix(line, " ")
}

func formatSize(bytes int64) string {
	units := " KMGT"
	i := 0
	for bytes >= 10000 && i < len(units)-1 {
		bytes = bytes/1024 + (bytes%1024+512)/1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%4d  ", bytes)
	}
	return fmt.Sprintf("%4d%cB", bytes, units[i])
}

func formatRate(bytes int64) string {
	units := " KMGT"
	hundredths := float64(bytes) * 100
	i := 0
	for hundredths >= 100000 && i < len(units)-1 {
		hundredths = math.Floor((hundredths + 512) / 1024)
		i++
	}
	if i == 0 {
		i = 1
		hundredths = math.Floor((hundredths + 512) / 1024)
	}
	h := int64(hundredths) + 5
	return fmt.Sprintf("%3d.%d%cB", h/100, h/10%10, units[i])
}

func utf8Locale() bool {
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := os.Getenv(key); value != "" {
			value = strings.ToUpper(value)
			return strings.Contains(value, "UTF-8") || strings.Contains(value, "UTF8")
		}
	}
	return false
}

// fit escapes nonprintable/invalid bytes as octal and counts display columns,
// not UTF-8 bytes. It bounds storage even for zero-width combining characters.
func fit(text string, columns int, utf8Mode, pad bool) string {
	if columns <= 0 {
		return ""
	}
	var out strings.Builder
	used := 0
	for len(text) > 0 && out.Len() < 4096 {
		r, n := utf8.DecodeRuneInString(text)
		unit, cells := "", 1
		if (!utf8Mode && text[0] >= 128) || (r == utf8.RuneError && n == 1) || !unicode.IsPrint(r) {
			unit, n, cells = fmt.Sprintf("\\%03o", text[0]), 1, 4
		} else {
			unit = text[:n]
			if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
				cells = 0
			} else {
				switch width.LookupRune(r).Kind() {
				case width.EastAsianWide, width.EastAsianFullwidth:
					cells = 2
				}
			}
		}
		if used+cells > columns || out.Len()+len(unit) > 4096 {
			break
		}
		out.WriteString(unit)
		used += cells
		text = text[n:]
	}
	if pad && used < columns {
		out.WriteString(strings.Repeat(" ", columns-used))
	}
	return out.String()
}
