package shellbroker

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// Adapter stages another independently authored in-memory Bash adapter (for
// example the forwarding adapter) through the same bootstrap, loader and
// framing as the command broker, without a command. It exposes the raw
// prefixed record channel; the adapter defines its own records.
type Adapter struct {
	plan   *Plan
	script string
}

// NewAdapter checks that tools are plain command names. requirement is the
// error reported when Bash >=4.3 or a tool is missing on the device.
func NewAdapter(script string, tools []string, requirement string) (*Adapter, error) {
	if len(script) == 0 || len(script) > 32768 {
		return nil, errors.New("adapter source must be 1..32768 bytes")
	}
	for _, tool := range tools {
		if tool == "" || strings.Trim(tool, "abcdefghijklmnopqrstuvwxyz0123456789-") != "" {
			return nil, errors.New("invalid adapter tool name")
		}
	}
	p, err := newPlan("", false)
	if err != nil {
		return nil, err
	}
	p.loader = strings.Replace(p.loader, "for x in cat setsid;", "for x in "+strings.Join(tools, " ")+";", 1)
	p.requirements = requirement
	return &Adapter{plan: p, script: script}, nil
}

// Records is the record channel of a started adapter. Send may be called
// concurrently; Next must have a single caller. Both use the Start context.
type Records struct{ w *wire }

func (r *Records) Send(record string) error { return r.w.send(record) }
func (r *Records) Next() (string, error)    { return r.w.next() }

// Start uploads the adapter and commits its execution. The carrier must stay
// open, and ctx live, for as long as the records are used.
func (a *Adapter) Start(ctx context.Context, stream Stream) (*Records, error) {
	w, err := a.plan.stage(ctx, stream, a.script)
	if err != nil {
		return nil, err
	}
	if err := w.send(fmt.Sprintf("G %d %d", len(a.script), 0)); err != nil {
		return nil, err
	}
	return &Records{w}, nil
}
