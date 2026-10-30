package sftpcli

import (
	"errors"
	"reflect"
	"testing"
)

type flushFault struct {
	events            []string
	syncErr, closeErr error
}

func (f *flushFault) Sync() error  { f.events = append(f.events, "sync"); return f.syncErr }
func (f *flushFault) Close() error { f.events = append(f.events, "close"); return f.closeErr }

func TestFlushCompletionOrderingAndFailures(t *testing.T) {
	copyErr, syncErr, closeErr := errors.New("copy"), errors.New("sync"), errors.New("close")
	for _, tt := range []struct {
		copyErr, syncErr, closeErr error
		flush, advisory            bool
		events                     []string
		want                       error
	}{
		{nil, nil, nil, true, false, []string{"sync", "close"}, nil},
		{nil, nil, nil, false, false, []string{"close"}, nil},
		{copyErr, nil, closeErr, false, false, []string{"close"}, copyErr},
		{copyErr, syncErr, closeErr, true, true, []string{"sync", "close"}, copyErr},
		{nil, syncErr, nil, true, false, []string{"sync", "close"}, syncErr},
		{nil, syncErr, nil, true, true, []string{"sync", "close"}, nil},
		{nil, syncErr, closeErr, true, true, []string{"sync", "close"}, closeErr},
	} {
		target := &flushFault{syncErr: tt.syncErr, closeErr: tt.closeErr}
		advised := false
		err := finishTransfer(target, tt.copyErr, tt.flush, func(err error) error {
			advised = true
			if tt.advisory {
				return nil
			}
			return err
		})
		if !reflect.DeepEqual(target.events, tt.events) || (tt.want == nil && err != nil) || (tt.want != nil && !errors.Is(err, tt.want)) || (tt.closeErr != nil && !errors.Is(err, tt.closeErr)) || advised != (tt.flush && tt.syncErr != nil) {
			t.Fatal("flush/close arbitration", target.events, err)
		}
	}
}

func TestTransferFlagBoundaries(t *testing.T) {
	for _, tt := range []struct {
		args  []string
		flush bool
		want  []string
	}{
		{[]string{"-ff", "--", "-f", "target"}, true, []string{"-f", "target"}},
		{[]string{"--", "-f", "target"}, false, []string{"-f", "target"}},
		{[]string{"source", "-f"}, false, []string{"source", "-f"}},
		{[]string{"-", "target"}, false, []string{"-", "target"}},
	} {
		got, options, err := transferFlags(tt.args, transferOptions{})
		if err != nil || options.resume || options.preserve || options.flush != tt.flush || !reflect.DeepEqual(got, tt.want) {
			t.Fatal("transfer argument boundary", got, options, err)
		}
	}
	for _, args := range [][]string{{"-z", "file"}, {"-fz", "file"}, {"-Z", "file"}, {"--f", "file"}} {
		if _, _, err := transferFlags(args, transferOptions{}); err == nil {
			t.Fatal("unsupported flags accepted", args)
		}
	}
}
