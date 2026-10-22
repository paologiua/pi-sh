//go:build darwin || linux

package sftpcli

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestUploadPreservationStaysOnOpenedFileAfterPathReplacement(t *testing.T) {
	client, _ := preservationServer(t)
	dir := t.TempDir()
	name, moved := filepath.Join(dir, "target"), filepath.Join(dir, "opened-inode")
	file, err := client.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := file.Write([]byte("original")); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(name, moved); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(name, []byte("replacement"), 0600); err != nil {
		t.Fatal(err)
	}
	originalTime := time.Unix(1500000000, 0)
	if err := os.Chtimes(name, originalTime, originalTime); err != nil {
		t.Fatal(err)
	}
	metadata := preservedMetadata{mode: 0640, atime: time.Unix(1600000001, 0), mtime: time.Unix(1600000013, 0)}
	console := Console{Client: client}
	if err := console.preserveRemote(file, metadata); err != nil {
		t.Fatal(err)
	}
	opened, err := os.Stat(moved)
	if err != nil {
		t.Fatal(err)
	}
	access, ok := localAccessTime(opened)
	if !ok || opened.Mode().Perm() != 0640 || access.Unix() != metadata.atime.Unix() || opened.ModTime().Unix() != metadata.mtime.Unix() {
		t.Fatal("opened file did not receive metadata")
	}
	replaced, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	replacementAccess, ok := localAccessTime(replaced)
	if !ok || replaced.Mode().Perm() != 0600 || replacementAccess.Unix() != originalTime.Unix() || replaced.ModTime().Unix() != originalTime.Unix() {
		t.Fatal("replacement file was modified through old path")
	}
}
