package sftpclient

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Detect accidental edits to the audited upstream snapshot. The manifest is
// reviewable provenance, not a signature or a substitute for supply-chain review.
func TestLocalSFTPSnapshotMatchesUpstreamManifest(t *testing.T) {
	root := filepath.Join("..", "..", "third_party", "sftp")
	data, err := os.ReadFile(filepath.Join(root, "UPSTREAM.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Module, Version string
		Files           map[string]string
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	if manifest.Module != "github.com/pkg/sftp" || manifest.Version != "v1.13.11" || len(manifest.Files) == 0 {
		t.Fatal("unrecognized snapshot provenance")
	}
	for name, want := range manifest.Files {
		if filepath.ToSlash(filepath.Clean(name)) != name || strings.HasPrefix(name, "../") || filepath.IsAbs(name) {
			t.Fatal("invalid provenance path")
		}
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != want {
			t.Fatalf("upstream source changed without review: %s", name)
		}
	}
	if err := filepath.Walk(root, func(name string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || filepath.Ext(name) != ".go" {
			return nil
		}
		relative, err := filepath.Rel(root, name)
		if err != nil {
			return err
		}
		relative = filepath.ToSlash(relative)
		if _, ok := manifest.Files[relative]; !ok && relative != "client_stream.go" && relative != "client_stream_test.go" && relative != "client_metadata.go" && relative != "client_metadata_test.go" {
			t.Errorf("unrecorded local source addition: %s", relative)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}
