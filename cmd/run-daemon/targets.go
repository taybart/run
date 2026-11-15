package main

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/taybart/run"
)

// Target is one connected run instance. Each connection is its own target,
// so two instances of the same command in the same directory are both
// listed, both restarted, and one leaving doesn't unregister the other.
type Target struct {
	key    string // makeKey of the command
	script string
	args   []string
	bus    *run.Bus

	mu          sync.Mutex
	pending     map[int]chan run.ResetResult // restarts waiting on run's reply
	logsPending map[int]chan run.LogsResult  // log requests waiting on run's reply
	gone        chan struct{}                // closed when it leaves
}

func newTarget(p run.Process, bus *run.Bus) *Target {
	return &Target{
		key: makeKey(p), script: p.Script, args: p.Args, bus: bus,
		pending:     map[int]chan run.ResetResult{},
		logsPending: map[int]chan run.LogsResult{},
		gone:        make(chan struct{}),
	}
}

func (t *Target) send(msg run.Message) error {
	return t.bus.Write(msg)
}

// requestIDs are unique across targets (0 means none, to older runs)
var requestIDs atomic.Int64

// reset restarts the instance and waits for its reply (how the restart went)
func (t *Target) reset(timeout time.Duration) run.ResetResult {
	id := int(requestIDs.Add(1))
	ch := make(chan run.ResetResult, 1)
	t.mu.Lock()
	t.pending[id] = ch
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.pending, id)
		t.mu.Unlock()
	}()

	if err := t.send(run.Message{ID: id, Action: run.ActionReset}); err != nil {
		return run.ResetResult{Error: "could not reach run: " + err.Error()}
	}
	select {
	case res := <-ch:
		return res
	case <-t.gone:
		return run.ResetResult{Error: "run exited during the restart"}
	case <-time.After(timeout):
		return run.ResetResult{Error: "sent the restart, but run didn't confirm it (an older run?)"}
	}
}

// replied delivers run's reply to restart id
func (t *Target) replied(id int, res run.ResetResult) {
	t.mu.Lock()
	ch := t.pending[id]
	t.mu.Unlock()
	if ch != nil {
		ch <- res // (buffered, one reply per id)
	}
}

// logs asks the instance for the tail of its output; run replies right away
func (t *Target) logs(timeout time.Duration) run.LogsResult {
	id := int(requestIDs.Add(1))
	ch := make(chan run.LogsResult, 1)
	t.mu.Lock()
	t.logsPending[id] = ch
	t.mu.Unlock()
	defer func() {
		t.mu.Lock()
		delete(t.logsPending, id)
		t.mu.Unlock()
	}()

	if err := t.send(run.Message{ID: id, Action: run.ActionLogs}); err != nil {
		return run.LogsResult{Error: "could not reach run: " + err.Error()}
	}
	select {
	case res := <-ch:
		return res
	case <-t.gone:
		return run.LogsResult{Error: "run exited"}
	case <-time.After(timeout):
		return run.LogsResult{Error: "run didn't reply (an older run?)"}
	}
}

// repliedLogs delivers run's reply to a logs request
func (t *Target) repliedLogs(id int, res run.LogsResult) {
	t.mu.Lock()
	ch := t.logsPending[id]
	t.mu.Unlock()
	if ch != nil {
		ch <- res
	}
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

// resetTimeout is how long a restart waits for run's reply, run replies
// run.ResetSettle after restarting (plus the time it takes to stop the old one)
var resetTimeout = run.ResetSettle + 4*time.Second

// logsTimeout is how long a logs request waits for run's reply, which it
// sends right away
var logsTimeout = 5 * time.Second

// restart restarts every instance of p's command in p.Dir, and returns how
// each went
func (ts *targets) restart(p run.Process) ([]run.ResetResult, error) {
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
		return nil, errNotFound
	}
	results := make([]run.ResetResult, len(matches))
	var wg sync.WaitGroup
	for i, t := range matches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = t.reset(resetTimeout)
		}()
	}
	wg.Wait()
	return results, nil
}

// logs returns the tail of what each instance of p's command in p.Dir printed
func (ts *targets) logs(p run.Process) ([]run.LogsResult, error) {
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
		return nil, errNotFound
	}
	results := make([]run.LogsResult, len(matches))
	var wg sync.WaitGroup
	for i, t := range matches {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i] = t.logs(logsTimeout)
		}()
	}
	wg.Wait()
	return results, nil
}
