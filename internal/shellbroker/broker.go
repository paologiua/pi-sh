// Package shellbroker implements a file-free Bash broker over an existing
// Connect shell. Its framing is pi-sh application data, not an agent protocol.
package shellbroker

import (
	"bytes"
	"context"
	"crypto/rand"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

//go:embed broker.bash
var source string

const maxRecord = 2304
const inputChunk = 1024

var ErrProtocol = errors.New("invalid or incomplete shell broker exchange")

// ErrCleanupUnconfirmed means the client could not observe the broker's
// acknowledgement after requesting abnormal process-group cleanup.
var ErrCleanupUnconfirmed = errors.New("remote broker cleanup was not confirmed")

const cleanupTimeout = 2 * time.Second

type Stream interface {
	Send(context.Context, []byte) error
	Receive(context.Context) ([]byte, error)
}

type IO struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	// StopIO promptly interrupts borrowed streams. When supplied, Run joins its
	// receive/input workers before returning; caller files remain caller-owned.
	StopIO func()
}
type Plan struct{ text, prefix, bootstrap, loader, requirements string }

func New(text string) (*Plan, error) {
	return newPlan(text, false)
}

func newPlan(text string, tty bool) (*Plan, error) {
	if !utf8.ValidString(text) || strings.IndexByte(text, 0) >= 0 || len(text) > 128*1024 {
		return nil, errors.New("command must be UTF-8 without NUL and at most 128 KiB")
	}
	var nonce [16]byte
	if _, err := rand.Read(nonce[:]); err != nil {
		return nil, errors.New("broker marker generation failed")
	}
	n := hex.EncodeToString(nonce[:])
	p := &Plan{text: text, prefix: "\x1ePISH3:" + n + ":"}
	tools := "cat setsid"
	p.requirements = "shell broker requires existing Bash >=4.3, cat and setsid"
	if tty {
		tools = "cat stty"
		p.requirements = "TTY adapter requires existing Bash >=4.3, cat and stty"
	}
	loader := `_ps_old_lc=${LC_ALL-};_ps_lc_set=${LC_ALL+x};export LC_ALL=C;printf -v _ps_prefix '\036PISH3:@N@:';_ps_emit(){ printf '%s%s\n' "$_ps_prefix" "$*"; };if ((BASH_VERSINFO[0]<4||(BASH_VERSINFO[0]==4&&BASH_VERSINFO[1]<3)));then _ps_emit 'Z bash';exit 125;fi;for x in cat setsid;do command -v "$x" >/dev/null||{ _ps_emit 'Z tools';exit 125; };done;_ps_emit B;code=;_ps_command=;l=0;c=0;while IFS= read -r line;do [[ $line == "$_ps_prefix"* ]]||exit 125;line=${line#"$_ps_prefix"};`
	// No here-string is used: parse bounded ASCII fields with parameter expansion.
	loader += `k=${line%% *};line=${line#* };s=${line%% *};h=${line#* };if [[ $k == G ]];then [[ ${#code} == "$s" && ${#_ps_command} == "$h" ]]||exit 125;eval "$code";exit;fi;[[ ${#h} -le 1792 && $h != *[!0123456789abcdef]* && $((${#h}%2)) == 0 ]]||exit 125;e=;for ((i=0;i<${#h};i+=2));do e+="\x${h:i:2}";done;printf -v d '%b' "$e";case $k in L)((l++));[[ $s == "$l" ]]||exit 125;code+=$d;;C)((c++));[[ $s == "$c" ]]||exit 125;_ps_command+=$d;;*)exit 125;;esac;((${#code}<=32768&&${#_ps_command}<=131072))||exit 125;_ps_emit "A $k $s";done`
	loader = strings.ReplaceAll(loader, "@N@", n)
	loader = strings.Replace(loader, "for x in cat setsid;", "for x in "+tools+";", 1)
	p.loader = loader
	stage0 := "printf '\\036PISH3:" + n + ":P\\n'; IFS= read -r b; eval \"$b\""
	p.bootstrap = " unset HISTFILE; stty raw -echo || exit; exec bash --noprofile --norc -c " + quote(stage0) + "\r"
	if tty {
		// Interactive initialization normalizes inherited job-control signals.
		// Skip startup files and isolate history before reading adapter input.
		p.bootstrap = " unset HISTFILE; _ps_saved=$(stty -g) || exit; stty raw -echo || exit; HISTFILE=/dev/null exec bash --noprofile --norc -im -c " + quote("set +H; set +o history; export -n HISTFILE; _ps_tty_saved=$1; "+stage0) + " bash \"$_ps_saved\"\r"
	}
	if len(p.bootstrap) > 512 {
		return nil, errors.New("broker bootstrap exceeds local canonical-line policy")
	}
	return p, nil
}

func quote(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }

type wire struct {
	ctx       context.Context
	stream    Stream
	prefix    string
	pending   []byte
	started   bool
	discarded int
	mu        sync.Mutex
}

func (w *wire) send(record string) error {
	if len(record) > maxRecord {
		return ErrProtocol
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.stream.Send(w.ctx, []byte(w.prefix+record+"\n"))
}
func (w *wire) next() (string, error) {
	for {
		if !w.started {
			i := bytes.Index(w.pending, []byte(w.prefix))
			if i >= 0 {
				w.discarded += i
				w.pending = w.pending[i:]
				w.started = true
			} else {
				n := max(0, len(w.pending)-len(w.prefix)+1)
				w.discarded += n
				w.pending = w.pending[n:]
			}
			if w.discarded > 64*1024 {
				return "", ErrProtocol
			}
		}
		if w.started {
			if i := bytes.IndexByte(w.pending, '\n'); i >= 0 {
				line := w.pending[:i]
				w.pending = w.pending[i+1:]
				if len(line) > maxRecord+len(w.prefix) || !bytes.HasPrefix(line, []byte(w.prefix)) {
					return "", ErrProtocol
				}
				return string(line[len(w.prefix):]), nil
			}
			if len(w.pending) > maxRecord+len(w.prefix) {
				return "", ErrProtocol
			}
		}
		chunk, err := w.stream.Receive(w.ctx)
		if err != nil {
			return "", err
		}
		w.pending = append(w.pending, chunk...)
	}
}

func (p *Plan) upload(w *wire, kind, text string) error {
	seq := 0
	for offset := 0; offset < len(text); offset += 896 {
		seq++
		part := text[offset:min(offset+896, len(text))]
		if err := w.send(fmt.Sprintf("%s %d %s", kind, seq, hex.EncodeToString([]byte(part)))); err != nil {
			return err
		}
		ack, err := w.next()
		if err != nil {
			return err
		}
		if ack != fmt.Sprintf("A %s %d", kind, seq) {
			return ErrProtocol
		}
	}
	return nil
}

type readResult struct {
	data []byte
	err  error
}
type recordResult struct {
	text string
	err  error
}

// stage uploads adapter source and command bytes without committing execution.
func (p *Plan) stage(ctx context.Context, stream Stream, script string) (*wire, error) {
	w := &wire{ctx: ctx, stream: stream, prefix: p.prefix}
	if err := stream.Send(ctx, []byte(p.bootstrap)); err != nil {
		return nil, err
	}
	pre, err := w.next()
	if err != nil {
		return nil, err
	}
	if pre != "P" {
		return nil, ErrProtocol
	}
	if err = stream.Send(ctx, []byte(p.loader+"\n")); err != nil {
		return nil, err
	}
	boot, err := w.next()
	if err != nil {
		return nil, err
	}
	if boot != "B" {
		return nil, errors.New(p.requirements)
	}
	if err = p.upload(w, "L", script); err != nil {
		return nil, err
	}
	if err = p.upload(w, "C", p.text); err != nil {
		return nil, err
	}
	return w, nil
}

// Run owns neither caller files nor the carrier. Stream methods must honor their
// contexts. Callers must arrange cancellation of blocking stdin/output I/O. After
// launch is committed, failures attempt a bounded cleanup acknowledgement before
// returning. The carrier must stay alive for that attempt to succeed.
func (p *Plan) Run(ctx context.Context, stream Stream, stdio IO) (code int, result error) {
	ctx, cancel := context.WithCancel(ctx)
	var workers sync.WaitGroup
	var stopOnce sync.Once
	stopIO := func() {
		cancel()
		if stdio.StopIO != nil {
			stopOnce.Do(stdio.StopIO)
		}
	}
	if stdio.StopIO != nil {
		// Output writes occur in the record loop itself; cancellation must
		// interrupt them before that loop can enter its deferred abort path.
		stopWatch := context.AfterFunc(ctx, func() { stopOnce.Do(stdio.StopIO) })
		defer stopWatch()
	}
	defer func() {
		stopIO()
		if stdio.StopIO != nil {
			workers.Wait()
		}
	}()
	if stdio.Stdout == nil {
		stdio.Stdout = io.Discard
	}
	if stdio.Stderr == nil {
		stdio.Stderr = io.Discard
	}
	w, err := p.stage(ctx, stream, source)
	if err != nil {
		return 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	// Keep just the receive pump alive during a bounded abort handshake. Normal
	// writes still use the caller's context, so backpressure remains cancellable.
	receiveCtx, stopReceive := context.WithCancel(context.WithoutCancel(ctx))
	defer stopReceive()
	receiver := &wire{ctx: receiveCtx, stream: stream, prefix: p.prefix, pending: w.pending, started: w.started, discarded: w.discarded}
	records := make(chan recordResult, 16)
	workers.Add(1)
	go func() {
		defer workers.Done()
		for {
			text, err := receiver.next()
			select {
			case records <- recordResult{text, err}:
			case <-receiveCtx.Done():
				return
			}
			if err != nil {
				return
			}
		}
	}()
	defer func() {
		// End local reads/writes before abnormal cleanup, while retaining the
		// independent receive pump and carrier for the C/D acknowledgment.
		stopIO()
		if result != nil {
			if err := p.abort(stream, records); err != nil {
				result = errors.Join(result, err)
			}
		}
	}()
	// Install cleanup before committing launch: Send can fail after the peer
	// accepted G, so a send failure is not proof that no worker was started.
	if err = w.send(fmt.Sprintf("G %d %d", len(source), len(p.text))); err != nil {
		return 0, err
	}
	// G may already have launched the worker even if cancellation wins before
	// the R record is read. Install abort handling before waiting for readiness.
	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case ready := <-records:
		if ready.err != nil {
			return 0, ready.err
		}
		if ready.text != "R" {
			return 0, ErrProtocol
		}
	}
	reads := make(chan readResult, 1)
	request := make(chan struct{}, 1)
	if stdio.Stdin != nil {
		workers.Add(1)
		go func() {
			defer workers.Done()
			buffer := make([]byte, inputChunk)
			for {
				select {
				case <-request:
				case <-ctx.Done():
					return
				}
				n, err := stdio.Stdin.Read(buffer)
				r := readResult{append([]byte(nil), buffer[:n]...), err}
				select {
				case reads <- r:
				case <-ctx.Done():
					return
				}
				if err != nil {
					return
				}
			}
		}()
		request <- struct{}{}
	} else {
		if err = w.send("F"); err != nil {
			return 0, err
		}
	}
	inSeq, outSeq, errSeq := 0, 0, 0
	waitingInput, inputDone := false, stdio.Stdin == nil
	var readErr error
	outEOF, errEOF := false, false
	for {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case r := <-reads:
			if inputDone {
				continue
			}
			if waitingInput {
				return 0, ErrProtocol
			}
			if len(r.data) > 0 {
				inSeq++
				waitingInput = true
				readErr = r.err
				if err = w.send(fmt.Sprintf("I %d %s", inSeq, hex.EncodeToString(r.data))); err != nil {
					return 0, err
				}
			} else if r.err != nil {
				if r.err != io.EOF {
					return 0, errors.New("local stdin read failed")
				}
				inputDone = true
				if err = w.send("F"); err != nil {
					return 0, err
				}
			} else {
				request <- struct{}{}
			}
		case r := <-records:
			if r.err != nil {
				return 0, fmt.Errorf("shell broker carrier failed: %w", r.err)
			}
			f := strings.Fields(r.text)
			if len(f) == 0 {
				return 0, ErrProtocol
			}
			switch f[0] {
			case "O", "E":
				if len(f) != 3 {
					return 0, ErrProtocol
				}
				seq, e := strconv.Atoi(f[1])
				if e != nil || strconv.Itoa(seq) != f[1] || len(f[2]) > 256 {
					return 0, ErrProtocol
				}
				data, e := hex.DecodeString(f[2])
				if e != nil || hex.EncodeToString(data) != f[2] {
					return 0, ErrProtocol
				}
				target := stdio.Stdout
				if f[0] == "O" {
					if outEOF || seq != outSeq+1 {
						return 0, ErrProtocol
					}
					outSeq = seq
				} else {
					if errEOF || seq != errSeq+1 {
						return 0, ErrProtocol
					}
					errSeq = seq
					target = stdio.Stderr
				}
				n, e := target.Write(data)
				if e != nil {
					return 0, e
				}
				if n != len(data) {
					return 0, io.ErrShortWrite
				}
				if err = w.send("A " + f[0] + " " + f[1]); err != nil {
					return 0, err
				}
			case "F":
				if len(f) != 2 {
					return 0, ErrProtocol
				}
				if f[1] == "O" && !outEOF {
					outEOF = true
				} else if f[1] == "E" && !errEOF {
					errEOF = true
				} else {
					return 0, ErrProtocol
				}
			case "A":
				if len(f) != 2 || f[1] != strconv.Itoa(inSeq) || !waitingInput || inputDone {
					return 0, ErrProtocol
				}
				waitingInput = false
				if readErr != nil {
					if readErr != io.EOF {
						return 0, errors.New("local stdin read failed")
					}
					inputDone = true
					if err = w.send("F"); err != nil {
						return 0, err
					}
				} else {
					request <- struct{}{}
				}
			case "K":
				inputDone = true
				waitingInput = false
			case "X":
				if len(f) != 2 || !outEOF || !errEOF {
					return 0, ErrProtocol
				}
				code, e := strconv.Atoi(f[1])
				if e != nil || code < 0 || code > 255 || strconv.Itoa(code) != f[1] {
					return 0, ErrProtocol
				}
				return code, nil
			case "Y":
				if len(f) != 2 || !outEOF || !errEOF {
					return 0, ErrProtocol
				}
				sig, e := strconv.Atoi(f[1])
				if e != nil || sig < 1 || sig > 127 || strconv.Itoa(sig) != f[1] {
					return 0, ErrProtocol
				}
				// OpenSSH leaves the client exit status at 255 for a direct
				// remote exit-signal, distinct from an ordinary exit 128+N.
				return 255, nil
			default:
				return 0, ErrProtocol
			}
		}
	}
}

// abort never reports remote command success. Late output is drained without
// writing to caller streams; only D confirms that abnormal cleanup finished.
func (p *Plan) abort(stream Stream, records <-chan recordResult) error {
	ctx, cancel := context.WithTimeout(context.Background(), cleanupTimeout)
	defer cancel()
	w := &wire{ctx: ctx, stream: stream, prefix: p.prefix}
	if err := w.send("C"); err != nil {
		return ErrCleanupUnconfirmed
	}
	for {
		select {
		case <-ctx.Done():
			return ErrCleanupUnconfirmed
		case r := <-records:
			if r.err != nil {
				return ErrCleanupUnconfirmed
			}
			if r.text == "D" {
				return nil
			}
		}
	}
}
