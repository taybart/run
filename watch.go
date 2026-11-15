package run

import (
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/taybart/log"
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

// watch sends on reloadch when files under dir w/ one of exts change (are
// added, edited or removed). exts are w/o the dot, "*" matches every file.
func watch(dir string, exts []string, interval time.Duration, reloadch chan<- bool) {
	prev := scan(dir, exts)
	pending := ""
	for range time.Tick(interval) {
		cur := scan(dir, exts)
		if p, ok := changed(prev, cur); ok {
			prev = cur
			if pending == "" {
				pending = p
			}
			continue // wait for it to settle
		}
		if pending != "" {
			log.Infof("%s\n%s changed%s\n", log.Yellow, pending, log.Rtd)
			pending = ""
			reloadch <- true
		}
	}
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
