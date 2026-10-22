package sshconfig

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configFile(t *testing.T, dir, name, text string) string {
	t.Helper()
	file := filepath.Join(dir, name)
	if err := os.WriteFile(file, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return file
}
func includeName(name string) string { return fmt.Sprintf("%q", filepath.ToSlash(name)) }

func TestIncludeScopeOrderingAndInactiveValidation(t *testing.T) {
	dir := t.TempDir()
	child := configFile(t, dir, "child", "Host other\nStdinNull yes\n")
	second := configFile(t, dir, "second", "EscapeChar !\n")
	root := configFile(t, dir, "root", "Host pi\nInclude "+includeName(child)+" "+includeName(second)+"\nRequestTTY force\n")
	got, err := Load(root, "pi")
	if err != nil || got["stdinnull"] != "" || got["escapechar"] != "!" || got["requesttty"] != "force" {
		t.Fatal("Include context leaked between files or to parent", got, err)
	}
	configFile(t, dir, "child", "Host *\nStdinNull yes\n")
	configFile(t, dir, "root", "Host other\nInclude "+includeName(child)+"\nHost *\nEscapeChar none\n")
	got, err = Load(root, "pi")
	if err != nil || got["stdinnull"] != "" || got["escapechar"] != "none" {
		t.Fatal("inactive Include reactivated Host", got, err)
	}
	configFile(t, dir, "child", "ProxyCommand hidden-unsupported\n")
	if _, err := Load(root, "pi"); err == nil || strings.Contains(err.Error(), "hidden") {
		t.Fatal("inactive unsupported semantics accepted or disclosed", err)
	}
}

func TestIncludeGlobsAndNestedFirstValues(t *testing.T) {
	dir := t.TempDir()
	configFile(t, dir, "20.conf", "EscapeChar none\nStdinNull no\n")
	leaf := configFile(t, dir, "leaf", "RequestTTY no\n")
	configFile(t, dir, "10.conf", "EscapeChar !\nInclude "+includeName(leaf)+"\n")
	configFile(t, dir, ".hidden.conf", "EscapeChar invalid-hidden-value\n")
	root := configFile(t, dir, "root", "Include "+includeName(filepath.Join(dir, "*.conf"))+"\nStdinNull yes\n")
	got, err := Load(root, "alias")
	if err != nil || got["escapechar"] != "!" || got["requesttty"] != "no" || got["stdinnull"] != "no" {
		t.Fatal("glob order/nesting/first values", got, err)
	}
	configFile(t, dir, "root", "Include "+includeName(filepath.Join(dir, "[!2]*.conf"))+"\n")
	got, err = Load(root, "alias")
	if err != nil || got["escapechar"] != "!" || got["stdinnull"] != "" {
		t.Fatal("negated class or dot matching", got, err)
	}
	configFile(t, dir, "root", "Include "+includeName(filepath.Join(dir, "[.]hidden.conf"))+" "+includeName(filepath.Join(dir, "missing-*"))+"\n")
	if _, err := Load(root, "alias"); err != nil {
		t.Fatal("dot class or no-match semantics", err)
	}
}

func TestIncludeExpansionUsesOriginalAndEffectiveNames(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PI_SH_TEST_INCLUDE_DIR", filepath.ToSlash(dir))
	configFile(t, dir, "alias", "StdinNull yes\n")
	configFile(t, dir, "device-alias", "EscapeChar !\n")
	root := configFile(t, dir, "root", "HostName device-%h\nInclude ${PI_SH_TEST_INCLUDE_DIR}/%h ${PI_SH_TEST_INCLUDE_DIR}/%n\n")
	got, err := Load(root, "alias")
	if err != nil || got["escapechar"] != "!" || got["stdinnull"] != "yes" {
		t.Fatal("Include token/environment expansion", got, err)
	}
	p := newParser(Context{Host: "alias"})
	p.home = dir
	for _, tt := range []struct{ in, want string }{
		{"part.conf", filepath.Join(dir, ".ssh", "part.conf")},
		{"~/part.conf", filepath.Join(dir, "part.conf")},
		{"%d/part.conf", filepath.Join(dir, "part.conf")},
		{"~/literal-%%-%n", filepath.Join(dir, "literal-%-alias")},
	} {
		got, err := p.includePath(tt.in)
		if err != nil || got != tt.want {
			t.Fatalf("%q: %q %v", tt.in, got, err)
		}
	}
	t.Setenv("PI_SH_TEST_INCLUDE_LITERAL", filepath.ToSlash(filepath.Join(dir, "%r")))
	name, err := p.includePath("${PI_SH_TEST_INCLUDE_LITERAL}")
	if err != nil || name != filepath.Join(dir, "%r") {
		t.Fatal("environment value recursively expanded", name, err)
	}
}

func TestIncludeBoundsAndErrorsAreAggregateAndRedacted(t *testing.T) {
	dir := t.TempDir()
	leaf := configFile(t, dir, "leaf-secret-name", "#\n")
	root := configFile(t, dir, "root", "Include "+strings.Repeat(includeName(leaf)+" ", MaxFiles)+"\n")
	if _, err := Load(root, "alias"); err == nil || !strings.Contains(err.Error(), "file count") || strings.Contains(err.Error(), dir) {
		t.Fatal("aggregate file bound", err)
	}
	configFile(t, dir, "root", "Include "+includeName(root)+"\n")
	if _, err := Load(root, "alias"); err == nil || !strings.Contains(err.Error(), "cycle") {
		t.Fatal("cycle accepted", err)
	}
	for i := 0; i <= MaxIncludeDepth+1; i++ {
		text := "# end\n"
		if i <= MaxIncludeDepth {
			text = "Include " + includeName(filepath.Join(dir, fmt.Sprint(i+1))) + "\n"
		}
		configFile(t, dir, fmt.Sprint(i), text)
	}
	if _, err := Load(filepath.Join(dir, "0"), "alias"); err == nil || !strings.Contains(err.Error(), "nesting") {
		t.Fatal("depth bound", err)
	}
	// Each source is within MaxBytes; their combined bytes exceed it.
	configFile(t, dir, "leaf-secret-name", strings.Repeat("# x\n", MaxBytes/8))
	configFile(t, dir, "root", "Include "+includeName(leaf)+" "+includeName(leaf)+"\n")
	if _, err := Load(root, "alias"); err == nil || !strings.Contains(err.Error(), "size limit") {
		t.Fatal("aggregate byte bound", err)
	}
	for _, text := range []string{"Include\n", "Include ~another-user/a\n", "Include %r\n", "Include ${PI_SH_TEST_UNSET_INCLUDE_VALUE_71}\n", "Include ${unterminated\n", "Include \"\"\n", "Include %\n", "Include ~/bad\x1bname\n"} {
		configFile(t, dir, "root", text)
		if _, err := Load(root, "alias"); err == nil || strings.Contains(err.Error(), dir) {
			t.Fatalf("invalid Include accepted or path leaked: %q %v", text, err)
		}
	}
	t.Setenv("PI_SH_TEST_INCLUDE_SECRET", filepath.ToSlash(dir))
	configFile(t, dir, "root", "Include ${PI_SH_TEST_INCLUDE_SECRET}\n")
	if _, err := Load(root, "alias"); err == nil || strings.Contains(err.Error(), dir) || strings.Contains(err.Error(), "PI_SH_TEST_INCLUDE_SECRET") {
		t.Fatal("nonregular path accepted or disclosed", err)
	}
	p := newParser(Context{Host: "alias"})
	p.entries = MaxGlobEntries
	if _, err := p.glob(leaf); err == nil {
		t.Fatal("filesystem visit limit ignored")
	}
	p = newParser(Context{Host: "alias"})
	p.entries = MaxGlobEntries - 10
	if _, err := p.glob(filepath.Join(dir, "*")); err == nil {
		t.Fatal("directory enumeration visit limit ignored")
	}
	if _, err := p.includePath(strings.Repeat("a", MaxPathBytes+1)); err == nil {
		t.Fatal("expanded path size limit ignored")
	}
}

func TestIncludeMatchBoundAndOverrideOwnership(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i <= MaxFiles; i++ {
		configFile(t, dir, fmt.Sprintf("%03d.conf", i), "#\n")
	}
	root := configFile(t, dir, "root", "Include "+includeName(filepath.Join(dir, "*.conf"))+"\n")
	if _, err := Load(root, "alias"); err == nil || !strings.Contains(err.Error(), "match count") {
		t.Fatal("glob match count not bounded", err)
	}
	configFile(t, dir, "device-alias", "EscapeChar !\n")
	configFile(t, dir, "root", "HostName ignored\nInclude "+includeName(filepath.Join(dir, "%h"))+"\n")
	initial := map[string]string{"hostname": "device-%h"}
	got, err := LoadWithOverrides(root, "alias", initial)
	if err != nil || got["escapechar"] != "!" || len(initial) != 1 || initial["hostname"] != "device-%h" {
		t.Fatal("overrides changed or not applied before Include", got, initial, err)
	}
}
