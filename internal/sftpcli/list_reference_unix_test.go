//go:build darwin || linux

package sftpcli

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestUnsortedListingAgainstLocalOpenSSH(t *testing.T) {
	executable, err := exec.LookPath("sftp")
	if err != nil {
		t.Skip("OpenSSH sftp unavailable")
	}
	client, server := preservationServer(t)
	root := t.TempDir()
	for _, name := range []string{"zeta", "alpha", "世界", "white space", "-leading"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	var ours bytes.Buffer
	console := Console{Client: client, Output: &ours}
	if _, err := console.Execute(context.Background(), []string{"ls", "-f", root}); err != nil {
		t.Fatal(err)
	}
	reference := exec.Command(executable, "-D", server, "-b", "-")
	reference.Stdin = strings.NewReader(fmt.Sprintf("@ls -1f %q\n@quit\n", root))
	output, err := reference.CombinedOutput()
	if err != nil {
		t.Fatalf("reference: %v %s", err, output)
	}
	// OpenSSH prints the requested directory prefix for each name. Our current
	// console deliberately keeps its documented basename-only presentation.
	var expected strings.Builder
	for _, line := range strings.Split(strings.TrimSuffix(string(output), "\n"), "\n") {
		if line == "" {
			continue
		}
		line = strings.TrimPrefix(line, root+"/")
		fmt.Fprintln(&expected, line)
	}
	if ours.String() != expected.String() {
		t.Fatalf("unsorted names/order differ: got %q want %q", ours.String(), expected.String())
	}
}
