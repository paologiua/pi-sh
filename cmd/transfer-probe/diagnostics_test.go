package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"pi-sh/internal/commandrun"
	"pi-sh/internal/shellbroker"
	"pi-sh/internal/transport"
)

func TestBrokerDiagnosticsClassifyWithoutExposingWrappedText(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want string
	}{
		{nil, "none"},
		{commandrun.ErrAuthorization, "session-check"},
		{context.Canceled, "canceled"},
		{context.DeadlineExceeded, "deadline"},
		{transport.ErrReceiveOverflow, "receive-overflow"},
		{transport.ErrMessageSize, "message-size"},
		{transport.ErrPeer, "peer"},
		{transport.ErrClosed, "transport-closed"},
		{shellbroker.ErrProtocol, "broker-protocol"},
		{io.ErrClosedPipe, "pipe-closed"},
		{io.ErrUnexpectedEOF, "unexpected-eof"},
		{io.EOF, "eof"},
		{errors.New("private-fixture-endpoint"), "other"},
	} {
		err := tc.err
		if err != nil {
			err = fmt.Errorf("private-fixture-message: %w", err)
			err = errors.Join(err, shellbroker.ErrCleanupUnconfirmed)
		}
		var out bytes.Buffer
		got := reportBroker(&out, 23, err, 1234*time.Millisecond)
		if got != err {
			t.Fatal("report changed the broker result")
		}
		wantClass := tc.want
		if tc.want == "other" {
			// The joined cleanup sentinel is known even if its primary error is not.
			wantClass = "cleanup-unconfirmed"
		}
		if !strings.Contains(out.String(), "failure="+wantClass+" ") || !strings.Contains(out.String(), "elapsed-ms=1234") || strings.Contains(out.String(), "private-fixture") {
			t.Fatal("unsafe or misleading diagnostic", out.String())
		}
	}
	if failureClass(errors.New("private-fixture")) != "other" {
		t.Fatal("unknown error text interpreted as a category")
	}
	if err := reportBroker(brokenReportWriter{}, 0, nil, 0); err == nil {
		t.Fatal("failed report write became success")
	}
	if err := reportBroker(brokenReportWriter{}, 0, io.EOF, 0); !errors.Is(err, io.EOF) {
		t.Fatal("failed report write erased broker failure")
	}
}

type brokenReportWriter struct{}

func (brokenReportWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
