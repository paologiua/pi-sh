package sshconfig

import (
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

func appendPath(base, component string) string {
	if strings.HasSuffix(base, string(os.PathSeparator)) {
		return base + component
	}
	return base + string(os.PathSeparator) + component
}

// glob enumerates in bounded chunks rather than filepath.Glob/ReadDir's complete
// directory allocation. Dot files need a literal leading dot, as in POSIX glob.
// Symlink/.. components are preserved for the filesystem to resolve.
func (p *parser) glob(pattern string) ([]string, error) {
	root := filepath.VolumeName(pattern) + string(os.PathSeparator)
	parts := strings.Split(strings.TrimPrefix(pattern, root), string(os.PathSeparator))
	if len(parts) > 64 {
		return nil, errors.New("Include path component limit exceeded")
	}
	paths := []string{root}
	for _, component := range parts {
		if component == "" {
			continue
		}
		var next []string
		wildcard := strings.ContainsAny(component, "*?[")
		matcher := component
		if wildcard {
			if strings.Contains(component, "[[:") || strings.Contains(component, "[[=") || strings.Contains(component, "[[.") {
				return nil, errors.New("Include POSIX named/collating classes are not implemented")
			}
			matcher = strings.ReplaceAll(component, "[!", "[^")
			if _, err := path.Match(matcher, ""); err != nil {
				return nil, errors.New("unsupported or malformed Include glob pattern")
			}
		}
		for _, base := range paths {
			if !wildcard {
				p.entries++
				if p.entries > MaxGlobEntries {
					return nil, errors.New("Include filesystem visit limit exceeded")
				}
				name := appendPath(base, component)
				if _, err := os.Lstat(name); err == nil {
					next = append(next, name)
				}
			} else {
				names, err := p.directoryMatches(base, component, matcher)
				if err != nil {
					return nil, err
				}
				next = append(next, names...)
			}
			if len(next) > MaxFiles {
				return nil, errors.New("Include match count exceeds limit")
			}
		}
		paths = next
	}
	sort.Strings(paths)
	return paths, nil
}

func (p *parser) directoryMatches(base, component, matcher string) ([]string, error) {
	// A literal preceding component may be a FIFO/device, not a directory.
	// Check before Open so ordinary special-file paths cannot block globbing.
	before, err := os.Stat(base)
	if err != nil || !before.IsDir() {
		return nil, nil
	}
	f, err := os.Open(base)
	if err != nil {
		return nil, nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.IsDir() {
		return nil, nil
	}
	var matches []string
	add := func(name string) error {
		p.entries++
		if p.entries > MaxGlobEntries {
			return errors.New("Include filesystem visit limit exceeded")
		}
		if strings.HasPrefix(name, ".") && !strings.HasPrefix(component, ".") {
			return nil
		}
		if yes, _ := path.Match(matcher, name); yes {
			full := appendPath(base, name)
			if len(full) > MaxPathBytes {
				return errors.New("Include path exceeds size limit")
			}
			matches = append(matches, full)
			if len(matches) > MaxFiles {
				return errors.New("Include match count exceeds limit")
			}
		}
		return nil
	}
	// POSIX glob includes these only when the pattern starts with a literal dot.
	if strings.HasPrefix(component, ".") {
		for _, name := range []string{".", ".."} {
			if err := add(name); err != nil {
				return nil, err
			}
		}
	}
	for {
		entries, err := f.ReadDir(128)
		for _, entry := range entries {
			if err := add(entry.Name()); err != nil {
				return nil, err
			}
		}
		if err != nil {
			if err != io.EOF {
				return nil, errors.New("Include directory enumeration failed")
			}
			break
		}
	}
	return matches, nil
}
