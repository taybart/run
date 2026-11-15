package run

import (
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// Watching polls the tree instead of using file events (inotify, kqueue):
// those need a watch per directory (linux) or an fd per file (macos), and
// lose files that editors save by renaming a temp file over them. Polling
// mtimes and sizes sees every change the same way everywhere, for a small
// walk each interval.

// pollInterval is how often the tree is checked, a change restarts the script
// once a check sees nothing new (so a burst of saves is one restart)
const pollInterval = 500 * time.Millisecond

type fileStat struct {
	mod  time.Time
	size int64
}

// Change is a file that was added, modified or removed
type Change struct {
	Kind string // added, modified, removed
	Path string // relative to the watched directory
}

// watch sends the changes to files under dir w/ one of exts (exts are w/o the
// dot, "*" matches every file), a batch once they settle: what's different
// from before the first change, so a burst of saves (or an editor's temp
// file + rename) is one batch.
func watch(dir string, exts []string, interval time.Duration, changech chan<- []Change) {
	base := scan(dir, exts) // (as of the last batch)
	prev, settling := base, false
	for range time.Tick(interval) {
		cur := scan(dir, exts)
		if _, ok := changed(prev, cur); ok {
			prev, settling = cur, true
			continue // wait for it to settle
		}
		if !settling {
			continue
		}
		settling = false
		changes := diff(dir, base, cur)
		base = cur
		if len(changes) > 0 {
			changech <- changes
		}
	}
}

// diff is how b differs from a, sorted by path
func diff(dir string, a, b map[string]fileStat) []Change {
	changes := []Change{}
	add := func(kind, p string) {
		if rel, err := filepath.Rel(dir, p); err == nil {
			p = rel
		}
		changes = append(changes, Change{Kind: kind, Path: p})
	}
	for p, s := range b {
		if old, ok := a[p]; !ok {
			add("added", p)
		} else if old != s {
			add("modified", p)
		}
	}
	for p := range a {
		if _, ok := b[p]; !ok {
			add("removed", p)
		}
	}
	slices.SortFunc(changes, func(x, y Change) int { return strings.Compare(x.Path, y.Path) })
	return changes
}

// describe is a short log line for a batch
func describe(changes []Change) string {
	if len(changes) == 1 {
		return changes[0].Path + " " + changes[0].Kind
	}
	return fmt.Sprintf("%d files changed", len(changes))
}

// scan stats the files under dir that match exts, skipping dot files and
// directories (ex. .git) and node_modules
func scan(dir string, exts []string) map[string]fileStat {
	all := slices.Contains(exts, "*")
	files := map[string]fileStat{}
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // (ex. removed during the walk)
		}
		name := d.Name()
		if path != dir && (strings.HasPrefix(name, ".") || name == "node_modules") {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.IsDir() || strings.HasSuffix(name, "~") {
			return nil
		}
		if !all && !slices.Contains(exts, strings.TrimPrefix(filepath.Ext(name), ".")) {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return nil
		}
		files[path] = fileStat{mod: info.ModTime(), size: info.Size()}
		return nil
	})
	return files
}

// changed returns a path that's different between a and b
func changed(a, b map[string]fileStat) (string, bool) {
	for p, s := range b {
		if old, ok := a[p]; !ok || old != s {
			return p, true
		}
	}
	for p := range a {
		if _, ok := b[p]; !ok {
			return p, true
		}
	}
	return "", false
}

// ParseExts splits a -w value ("mn,rest" or ".mn, .rest") into extensions
func ParseExts(s string) []string {
	exts := []string{}
	for e := range strings.SplitSeq(s, ",") {
		if e = strings.TrimPrefix(strings.TrimSpace(e), "."); e != "" {
			exts = append(exts, e)
		}
	}
	return exts
}
