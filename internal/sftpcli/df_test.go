package sftpcli

import (
	"bytes"
	"github.com/pkg/sftp"
	"testing"
)

func TestDfOutput(t *testing.T) {
	s := &sftp.StatVFS{Frsize: 1024, Blocks: 100, Bfree: 25, Bavail: 20, Files: 1000, Ffree: 400, Favail: 300}
	for _, tc := range []struct {
		human, inodes bool
		want          string
	}{
		{false, false, "        Size         Used        Avail       (root)    %Capacity\n         100           75           20           25          75%\n"},
		{true, false, "    Size     Used    Avail   (root)    %Capacity\n   100KB     75KB     20KB     25KB          75%\n"},
		{true, true, "     Inodes        Used       Avail      (root)    %Capacity\n       1000         600         300         400          60%\n"},
	} {
		var out bytes.Buffer
		if err := printDiskFree(&out, s, tc.human, tc.inodes); err != nil {
			t.Fatal(err)
		}
		if out.String() != tc.want {
			t.Fatalf("got %q want %q", out.String(), tc.want)
		}
	}
}
