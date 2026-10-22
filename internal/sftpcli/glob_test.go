package sftpcli

import (
	"bytes"
	"context"
	"reflect"
	"strings"
	"testing"
)

func TestSplitKeepsOpenSSHGlobEscaping(t *testing.T) {
	for line, want := range map[string][]string{
		`ls r/a*`:               {"ls", "r/a*"},
		`ls 'r/a*'`:             {"ls", `r/a\*`},
		`ls "r/[x]?"`:           {"ls", `r/\[x]\?`},
		`ls r/b\*x`:             {"ls", `r/b\*x`},
		`ls r/a\1`:              {"ls", "r/a1"},
		`ls r/back\\slash`:      {"ls", `r/back\\slash`},
		`ls 'r/back\slash'`:     {"ls", `r/back\slash`},
		`ls 'it\'s'`:            {"ls", "it's"},
		`ls "q\"*"`:             {"ls", `q"\*`},
		`ls "a\*"`:              {"ls", `a\\\*`},
		`ls a#b`:                {"ls", "a"},
		`ls 'a#b'`:              {"ls", "a#b"},
		"get\ta\vb":             {"get", "a", "b"},
		`put ''`:                {"put", ""},
		`put a'b c'd`:           {"put", "ab cd"},
		`# only a comment`:      nil,
		`rename 'x y' z\ w # c`: {"rename", "x y", "z w"},
	} {
		got, err := Split(line)
		if err != nil || !reflect.DeepEqual(got, want) {
			t.Errorf("Split(%q) = %q, %v; want %q", line, got, err, want)
		}
	}
}

func TestMultipleMatchFailuresAreEachReported(t *testing.T) {
	client := memoryClient(t)
	for _, name := range []string{"/d", "/d/sub1", "/d/sub2"} {
		if err := client.Mkdir(name); err != nil {
			t.Fatal(err)
		}
	}
	var errs bytes.Buffer
	c := Console{Client: client, RemoteDir: "/", LocalDir: t.TempDir(), Errors: &errs}
	_, err := c.Execute(context.Background(), []string{"get", "d/*"})
	if err == nil || !strings.Contains(err.Error(), "/d/sub2/: not a regular file") {
		t.Fatal("last failure not returned", err)
	}
	if !strings.Contains(errs.String(), "/d/sub1/: not a regular file") {
		t.Fatal("earlier failure not reported", errs.String())
	}
	if _, err := c.Execute(context.Background(), []string{"rm", "d/sub*"}); err == nil {
		t.Fatal("rm removed directories")
	}
	if _, err := client.Stat("/d/sub1"); err != nil {
		t.Fatal("rm removed a directory", err)
	}
}
