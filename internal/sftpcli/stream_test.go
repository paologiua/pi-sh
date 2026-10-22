package sftpcli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
)

func TestUploadWriteFailureNeverBecomesSuccess(t *testing.T) {
	for _, packet := range []int{1024, 32768} {
		for _, size := range []int{1, 1023, 1024, 1025, 8192, 32768, 32769} {
			t.Run(fmt.Sprintf("packet%d/bytes%d", packet, size), func(t *testing.T) {
				client := fsyncFixtureClient(t, "write-error", sftp.MaxPacketChecked(packet))
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "input"), bytes.Repeat([]byte("x"), size), 0600); err != nil {
					t.Fatal(err)
				}
				c := Console{Client: client, LocalDir: dir, RemoteDir: "/", Output: io.Discard}
				if _, err := c.Execute(context.Background(), []string{"put", "input", "remote"}); err == nil {
					t.Fatal("rejected WRITE reported success")
				}
				info, err := client.Stat("/remote")
				if err != nil || info.Size() != 0 {
					t.Fatal("rejecting fixture unexpectedly wrote data", err)
				}
			})
		}
	}
}

func TestLastShortWriteFailureAfterAcknowledgedPrefix(t *testing.T) {
	for _, packet := range []int{1024, 32768} {
		for _, size := range []int{1025, 8193, 32769} {
			t.Run(fmt.Sprintf("packet%d/bytes%d", packet, size), func(t *testing.T) {
				client := fsyncFixtureClient(t, "write-tail", sftp.MaxPacketChecked(packet))
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "input"), bytes.Repeat([]byte("x"), size), 0600); err != nil {
					t.Fatal(err)
				}
				c := Console{Client: client, LocalDir: dir, RemoteDir: "/", Output: io.Discard}
				if _, err := c.Execute(context.Background(), []string{"put", "input", "remote"}); err == nil {
					t.Fatal("last short WRITE failure became success")
				}
				info, err := client.Stat("/remote")
				if err != nil || info.Size() != 1024 {
					t.Fatal("partial-write fixture did not retain only its acknowledged prefix", err)
				}
			})
		}
	}
}
