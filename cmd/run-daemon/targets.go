package main

import (
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/taybart/run"
)

// Target is one connected run instance. Each connection is its own target,
// so two instances of the same command in the same directory are both
// listed, both restarted, and one leaving doesn't unregister the other.
type Target struct {
	key    string // makeKey of the command
	script string
	args   []string

	mu  sync.Mutex // the bus is written by its connection and http/mcp
	bus *run.Bus
}

func (t *Target) send(msg run.Message) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.bus.Write(msg)
}

// targets are the connected instances by directory, safe for the
// connections' and http handlers' goroutines
type targets struct {
	mu    sync.Mutex
	byDir map[string]map[*Target]bool
}

func newTargets() *targets {
	return &targets{byDir: map[string]map[*Target]bool{}}
}

func (ts *targets) add(dir string, t *Target) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	if ts.byDir[dir] == nil {
		ts.byDir[dir] = map[*Target]bool{}
	}
	ts.byDir[dir][t] = true
}

func (ts *targets) remove(dir string, t *Target) {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	delete(ts.byDir[dir], t)
	if len(ts.byDir[dir]) == 0 {
		delete(ts.byDir, dir)
	}
}

// list returns the instances in the directories match accepts, sorted
func (ts *targets) list(match func(dir string) bool) []run.Process {
	ts.mu.Lock()
	defer ts.mu.Unlock()
	res := []run.Process{}
	for dir, set := range ts.byDir {
		if !match(dir) {
			continue
		}
		for t := range set {
			res = append(res, run.Process{Dir: dir, Script: t.script, Args: t.args})
		}
	}
	sort.Slice(res, func(i, j int) bool {
		if res[i].Dir != res[j].Dir {
			return res[i].Dir < res[j].Dir
		}
		return strings.Join(append([]string{res[i].Script}, res[i].Args...), " ") <
			strings.Join(append([]string{res[j].Script}, res[j].Args...), " ")
	})
	return res
}

// errNotFound is when no instance of the command runs in the directory
var errNotFound = errors.New("no matching script found")

// restart resets every instance of p's command in p.Dir, returning how many
func (ts *targets) restart(p run.Process) (int, error) {
	key := makeKey(p)
	ts.mu.Lock()
	matches := []*Target{}
	for t := range ts.byDir[p.Dir] {
		if t.key == key {
			matches = append(matches, t)
		}
	}
	ts.mu.Unlock()
	if len(matches) == 0 {
		return 0, errNotFound
	}
	var errs []error
	for _, t := range matches {
		if err := t.send(run.Message{Action: run.ActionReset}); err != nil {
			errs = append(errs, err)
		}
	}
	return len(matches) - len(errs), errors.Join(errs...)
}
