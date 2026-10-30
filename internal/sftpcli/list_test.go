package sftpcli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/pkg/sftp"
)

type listingInfo string

func (i listingInfo) Name() string     { return string(i) }
func (listingInfo) Size() int64        { return 0 }
func (listingInfo) Mode() os.FileMode  { return 0600 }
func (listingInfo) ModTime() time.Time { return time.Unix(0, 0) }
func (listingInfo) IsDir() bool        { return false }
func (listingInfo) Sys() any           { return nil }

type listingFixture struct {
	sftp.FileLister
	count                 int
	calls, closed         atomic.Int64
	readError, closeError bool
}

func (f *listingFixture) Filelist(r *sftp.Request) (sftp.ListerAt, error) {
	if r.Method == "List" {
		return f, nil
	}
	return f.FileLister.Filelist(r)
}
func (f *listingFixture) ListAt(dst []os.FileInfo, offset int64) (int, error) {
	f.calls.Add(1)
	if f.readError && offset > 0 {
		return 0, sftp.ErrSSHFxPermissionDenied
	}
	n := min(len(dst), f.count-int(offset))
	if n <= 0 {
		return 0, io.EOF
	}
	for i := 0; i < n; i++ {
		dst[i] = listingInfo(fmt.Sprintf("entry-%08d", offset+int64(i)))
	}
	return n, nil
}
func (f *listingFixture) Close() error {
	f.closed.Add(1)
	if f.closeError {
		return sftp.ErrSSHFxPermissionDenied
	}
	return nil
}

func TestStreamingDirectoryStopsEarlyClosesAndRetainsFailures(t *testing.T) {
	for _, mode := range []string{"all", "visitor", "cancel", "read", "close", "visitor-and-close"} {
		t.Run(mode, func(t *testing.T) {
			fixture := &listingFixture{count: 10000, readError: mode == "read", closeError: mode == "close" || mode == "visitor-and-close"}
			client := memoryClientWith(t, func(h *sftp.Handlers) { fixture.FileLister = h.FileList; h.FileList = fixture })
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			sentinel := errors.New("visitor-failed")
			visits := 0
			err := client.ReadDirEach(ctx, "/", func(info os.FileInfo) error {
				if info.Name() != fmt.Sprintf("entry-%08d", visits) {
					t.Fatal("server ordering/name changed")
				}
				visits++
				// Requests inside the visitor must not deadlock the client.
				if visits == 1 {
					if _, err := client.Stat("/"); err != nil {
						return err
					}
				}
				if mode == "visitor" || mode == "visitor-and-close" {
					return sentinel
				}
				if mode == "cancel" {
					cancel()
				}
				return nil
			})
			if fixture.closed.Load() != 1 {
				t.Fatal("directory handle was not closed exactly once", fixture.closed.Load())
			}
			switch mode {
			case "all":
				if err != nil || visits != 10000 {
					t.Fatal("full streaming list failed", visits, err)
				}
			case "visitor", "visitor-and-close":
				if !errors.Is(err, sentinel) || visits != 1 || fixture.calls.Load() != 1 {
					t.Fatal("visitor failure was lost or listing prefetched", visits, fixture.calls.Load(), err)
				}
			case "cancel":
				if !errors.Is(err, context.Canceled) || visits != 1 || fixture.calls.Load() != 1 {
					t.Fatal("cancellation did not stop listing", visits, fixture.calls.Load(), err)
				}
			case "read":
				if !errors.Is(err, os.ErrPermission) || visits == 0 || visits == 10000 {
					t.Fatal("READDIR failure lost", visits, err)
				}
			case "close":
				if !errors.Is(err, os.ErrPermission) || visits != 10000 {
					t.Fatal("CLOSE failure lost", visits, err)
				}
			}
			if mode == "visitor-and-close" && !errors.Is(err, os.ErrPermission) {
				t.Fatal("joined close failure lost", err)
			}
		})
	}
}

func TestListingUnsortedStreamsAndRefusesUnknownFlags(t *testing.T) {
	fixture := &listingFixture{count: 10000}
	client := memoryClientWith(t, func(h *sftp.Handlers) { fixture.FileLister = h.FileList; h.FileList = fixture })
	var output bytes.Buffer
	c := Console{Client: client, RemoteDir: "/", Output: &output}
	if _, err := c.Execute(context.Background(), []string{"ls", "-1ff", "/"}); err != nil {
		t.Fatal(err)
	}
	if strings.Count(output.String(), "\n") != 10000 || !strings.HasPrefix(output.String(), "entry-00000000\n") {
		t.Fatal("listing incomplete")
	}
	for _, args := range [][]string{{"ls", "-z"}, {"ls", "-fz"}, {"ls", "-ff", "/", "extra"}} {
		empty := Console{}
		if _, err := empty.Execute(context.Background(), args); err == nil {
			t.Fatal("invalid listing accepted", args)
		}
	}
	args, unsorted, err := listingFlags([]string{"-f", "--", "-name"})
	if err != nil || !unsorted || len(args) != 1 || args[0] != "-name" {
		t.Fatal("literal operand changed", args, unsorted, err)
	}
}
