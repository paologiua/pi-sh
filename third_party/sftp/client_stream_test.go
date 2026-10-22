package sftp

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
)

func streamTestString(b []byte, s string) []byte {
	b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
	return append(b, s...)
}
func streamTestNames(names ...string) []byte {
	b := binary.BigEndian.AppendUint32(nil, 7)
	b = binary.BigEndian.AppendUint32(b, uint32(len(names)))
	for _, name := range names {
		b = streamTestString(b, name)
		b = streamTestString(b, "")
		b = binary.BigEndian.AppendUint32(b, 0)
	}
	return b
}
func streamTestStatus(code uint32) []byte {
	b := binary.BigEndian.AppendUint32(nil, 7)
	b = binary.BigEndian.AppendUint32(b, code)
	return streamTestString(streamTestString(b, "secret-server-text"), "en")
}

func TestStreamNamesValidationCancellationAndVisitorFailure(t *testing.T) {
	good := streamTestNames("世界", "line\nbreak", ".", "..", "-literal")
	var names []string
	if err := streamNames(context.Background(), 7, good, func(i os.FileInfo) error { names = append(names, i.Name()); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, "|") != "世界|line\nbreak|-literal" {
		t.Fatal("names changed", names)
	}
	for _, name := range []string{"", "dir/file", "../escape", "nul\x00name"} {
		visits := 0
		if err := streamNames(context.Background(), 7, streamTestNames(name), func(os.FileInfo) error { visits++; return nil }); err == nil || visits != 0 {
			t.Fatalf("invalid name reached visitor: %q", name)
		}
	}
	for n := 0; n < len(good); n++ {
		if err := streamNames(context.Background(), 7, good[:n], func(os.FileInfo) error { return nil }); err == nil {
			t.Fatalf("truncated packet accepted at %d", n)
		}
	}
	for _, bad := range [][]byte{streamTestNames(), append(append([]byte(nil), good...), 0), {0, 0, 0, 7, 255, 255, 255, 255}} {
		if err := streamNames(context.Background(), 7, bad, func(os.FileInfo) error { return nil }); err == nil {
			t.Fatal("invalid count/trailing packet accepted")
		}
	}
	if err := streamNames(context.Background(), 8, good, func(os.FileInfo) error { return nil }); err == nil {
		t.Fatal("wrong id accepted")
	}
	flags := streamTestNames("file")
	binary.BigEndian.PutUint32(flags[len(flags)-4:], 0x40000000)
	if err := streamNames(context.Background(), 7, flags, func(os.FileInfo) error { return nil }); err == nil {
		t.Fatal("unknown attribute flags accepted")
	}
	sentinel := errors.New("visitor-failed")
	if err := streamNames(context.Background(), 7, good, func(os.FileInfo) error { return sentinel }); !errors.Is(err, sentinel) {
		t.Fatal("visitor error lost", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	visits := 0
	err := streamNames(ctx, 7, good, func(os.FileInfo) error { visits++; cancel(); return nil })
	if !errors.Is(err, context.Canceled) || visits != 1 {
		t.Fatal("cancellation did not stop traversal", visits, err)
	}
}

func TestStreamStatusRejectsTruncationAndUnsolicitedSuccess(t *testing.T) {
	eof := streamTestStatus(1)
	if err := streamStatus(7, eof, false); !errors.Is(err, io.EOF) {
		t.Fatal(err)
	}
	if err := streamStatus(7, streamTestStatus(0), true); err != nil {
		t.Fatal(err)
	}
	if err := streamStatus(7, streamTestStatus(0), false); err == nil {
		t.Fatal("READDIR success without entries became EOF")
	}
	for n := 0; n < len(eof); n++ {
		if err := streamStatus(7, eof[:n], false); err == nil || errors.Is(err, io.EOF) {
			t.Fatalf("truncated EOF accepted at %d", n)
		}
	}
	if err := streamStatus(7, append(eof, 0), false); err == nil || errors.Is(err, io.EOF) {
		t.Fatal("trailing EOF accepted", err)
	}
	err := streamStatus(7, streamTestStatus(3), false)
	if !errors.Is(err, os.ErrPermission) || strings.Contains(err.Error(), "secret") {
		t.Fatal("status classification/redaction failed", err)
	}
}

func FuzzStreamDirectoryPackets(f *testing.F) {
	for _, seed := range [][]byte{streamTestNames("file"), streamTestNames(".."), streamTestNames("dir/file"), streamTestStatus(1), {0, 0, 0, 7, 255, 255, 255, 255}} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		_ = streamNames(context.Background(), 7, data, func(os.FileInfo) error { return nil })
		_ = streamStatus(7, data, false)
	})
}
