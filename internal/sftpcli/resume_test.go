package sftpcli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/pkg/sftp"
)

type resumeCase struct{ direction, state, form string }

func resumeCases() []resumeCase {
	var cases []resumeCase
	for _, direction := range []string{"get", "put"} {
		for _, state := range []string{"missing", "empty", "partial", "complete", "larger", "wrong-prefix"} {
			for _, form := range []string{"flag", "alias", "global", "flush"} {
				cases = append(cases, resumeCase{direction, state, form})
			}
		}
	}
	return cases
}

func (tc resumeCase) name() string { return tc.direction + "/" + tc.state + "/" + tc.form }
func (tc resumeCase) args(source, target string) []string {
	name := tc.direction
	switch tc.form {
	case "alias":
		name = "re" + name
	case "flag":
		return []string{name, "-a", source, target}
	case "flush":
		return []string{name, "-aff", source, target}
	}
	return []string{name, source, target}
}

func resumeFixture(state string) (source, prefix, want []byte) {
	source = make([]byte, 8193)
	for i := range source {
		source[i] = byte(i)
	}
	switch state {
	case "empty", "missing":
		prefix = []byte{}
	case "partial":
		prefix = bytes.Clone(source[:1023])
	case "complete":
		prefix = bytes.Clone(source)
	case "larger":
		prefix = append(bytes.Clone(source), 'x')
	case "wrong-prefix":
		prefix = []byte("wrong")
	}
	want = append(bytes.Clone(prefix), source[min(len(prefix), len(source)):]...)
	return
}

func writeResumeRemote(t *testing.T, client *sftp.Client, name string, data []byte) {
	t.Helper()
	f, err := client.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := copyFileData(f, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func runResumeCase(t *testing.T, client *sftp.Client, remoteDir string, tc resumeCase) {
	t.Helper()
	dir := t.TempDir()
	source, prefix, want := resumeFixture(tc.state)
	local, remote := filepath.Join(dir, "local"), remoteDir+"/remote"
	localData, remoteData := prefix, source
	if tc.direction == "put" {
		localData, remoteData = source, prefix
	}
	if tc.direction == "put" || tc.state != "missing" {
		if err := os.WriteFile(local, localData, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if tc.direction == "get" || tc.state != "missing" {
		writeResumeRemote(t, client, remote, remoteData)
	}
	var diagnostic bytes.Buffer
	c := Console{Client: client, RemoteDir: remoteDir, LocalDir: dir, Resume: tc.form == "global", Errors: &diagnostic}
	a, b := remote, local
	if tc.direction == "put" {
		a, b = local, remote
	}
	_, err := c.Execute(context.Background(), tc.args(a, b))
	wantFailure := tc.state == "larger" || tc.direction == "put" && (tc.state == "missing" || tc.state == "complete")
	if (err != nil) != wantFailure {
		t.Fatalf("resume status: %v", err)
	}
	if wantFailure {
		want = prefix
	}
	var got []byte
	if tc.direction == "get" {
		got, err = os.ReadFile(local)
	} else {
		var f *sftp.File
		f, err = client.Open(remote)
		if err == nil {
			got, err = io.ReadAll(f)
			if e := f.Close(); e != nil {
				t.Fatal(e)
			}
		}
	}
	if tc.direction == "put" && tc.state == "missing" {
		if !os.IsNotExist(err) {
			t.Fatal("missing resume target created", err)
		}
		return
	}
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("resume data: got %d want %d, %v", len(got), len(want), err)
	}
}

func TestResumeMemoryServer(t *testing.T) {
	for _, tc := range resumeCases() {
		t.Run(tc.name(), func(t *testing.T) { runResumeCase(t, memoryClient(t), "", tc) })
	}
}

func TestResumeEmptySource(t *testing.T) {
	for _, direction := range []string{"get", "put"} {
		for _, exists := range []bool{false, true} {
			t.Run(direction+map[bool]string{false: "/missing", true: "/existing"}[exists], func(t *testing.T) {
				client := memoryClient(t)
				dir := t.TempDir()
				local := filepath.Join(dir, "local")
				if direction == "put" || exists {
					if err := os.WriteFile(local, nil, 0600); err != nil {
						t.Fatal(err)
					}
				}
				if direction == "get" || exists {
					writeResumeRemote(t, client, "/remote", nil)
				}
				c := Console{Client: client}
				a, b := "/remote", local
				if direction == "put" {
					a, b = local, "/remote"
				}
				_, err := c.Execute(context.Background(), []string{"re" + direction, a, b})
				if (err != nil) != (direction == "put") {
					t.Fatal("empty-source resume status", err)
				}
			})
		}
	}
}

func TestResumeFlagBoundaries(t *testing.T) {
	args, options, err := transferFlags([]string{"-afa", "--", "-a"}, transferOptions{})
	if err != nil || !options.flush || !options.resume || options.preserve || len(args) != 1 || args[0] != "-a" {
		t.Fatal(args, options, err)
	}
	args, options, err = transferFlags([]string{"source", "-a"}, transferOptions{})
	if err != nil || options.flush || options.resume || options.preserve || len(args) != 2 {
		t.Fatal("post-operand flag interpreted")
	}
	_, _, err = transferFlags([]string{"-az", "source"}, transferOptions{})
	if err == nil {
		t.Fatal("unsupported combined option accepted")
	}
}

func TestResumeUploadDiscardsUnacknowledgedTail(t *testing.T) {
	for _, prefixSize := range []int{0, 512} {
		t.Run(string(rune('a'+prefixSize/512)), func(t *testing.T) {
			client := fsyncFixtureClient(t, "write-tail", sftp.MaxPacketChecked(1024))
			prefix := bytes.Repeat([]byte("p"), prefixSize)
			writeResumeRemote(t, client, "/remote", prefix)
			dir := t.TempDir()
			data := bytes.Repeat([]byte("x"), 8193)
			if err := os.WriteFile(filepath.Join(dir, "input"), data, 0600); err != nil {
				t.Fatal(err)
			}
			var diagnostic bytes.Buffer
			c := Console{Client: client, LocalDir: dir, RemoteDir: "/", Errors: &diagnostic}
			if _, err := c.Execute(context.Background(), []string{"reput", "-f", "input", "remote"}); err == nil {
				t.Fatal("resume WRITE failure hidden")
			}
			f, err := client.Open("/remote")
			if err != nil {
				t.Fatal(err)
			}
			got, err := io.ReadAll(f)
			f.Close()
			want := prefix
			if prefixSize == 0 {
				want = data[:1024]
			}
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("unacknowledged tail retained: got %d want %d, %v", len(got), len(want), err)
			}
		})
	}
}

type resumeDenyRead struct {
	io.ReaderAt
	after int64
}

func (r resumeDenyRead) ReadAt(p []byte, off int64) (int, error) {
	if off >= r.after {
		return 0, os.ErrPermission
	}
	if int64(len(p)) > r.after-off {
		n, err := r.ReaderAt.ReadAt(p[:int(r.after-off)], off)
		if err == nil {
			err = io.EOF
		}
		return n, err
	}
	return r.ReaderAt.ReadAt(p, off)
}

type resumeDenyFileRead struct {
	sftp.FileReader
	after int64
}

func (r resumeDenyFileRead) Fileread(req *sftp.Request) (io.ReaderAt, error) {
	f, err := r.FileReader.Fileread(req)
	if err != nil {
		return nil, err
	}
	return resumeDenyRead{f, r.after}, nil
}

func TestResumeDownloadRetainsWrittenPrefixOnReadFailure(t *testing.T) {
	client := memoryClientWith(t, func(h *sftp.Handlers) { h.FileGet = resumeDenyFileRead{h.FileGet, 1536} })
	data := bytes.Repeat([]byte("x"), 8193)
	writeResumeRemote(t, client, "/remote", data)
	dir := t.TempDir()
	local := filepath.Join(dir, "target")
	prefix := bytes.Repeat([]byte("p"), 512)
	if err := os.WriteFile(local, prefix, 0600); err != nil {
		t.Fatal(err)
	}
	c := Console{Client: client, LocalDir: dir, RemoteDir: "/"}
	if _, err := c.Execute(context.Background(), []string{"reget", "remote", "target"}); err == nil {
		t.Fatal("resume READ failure hidden")
	}
	got, err := os.ReadFile(local)
	want := append(bytes.Clone(prefix), data[512:1536]...)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatal("original prefix lost", err)
	}
}
