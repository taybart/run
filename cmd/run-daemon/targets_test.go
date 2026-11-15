package main

import (
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/taybart/run"
)

// client is a fake run instance connected to the daemon over a pipe, it
// replies to resets w/ result (unless nil)
type client struct {
	conn   net.Conn
	bus    *run.Bus
	msgs   chan run.Message
	result *run.ResetResult
}

var running = &run.ResetResult{OK: true, Running: true, Output: "listening to :8080...\n"}

func join(t *testing.T, s *Server, p run.Process) *client {
	t.Helper()
	return joinWith(t, s, p, running)
}

func joinWith(t *testing.T, s *Server, p run.Process, result *run.ResetResult) *client {
	t.Helper()
	server, conn := net.Pipe()
	go s.handle(server)
	bus, _ := run.NewBus(conn)
	c := &client{conn: conn, bus: bus, msgs: make(chan run.Message, 10), result: result}
	go func() {
		for {
			msg, err := bus.Read()
			if err != nil {
				close(c.msgs)
				return
			}
			if msg.Action == run.ActionReset && c.result != nil {
				go bus.Write(run.Message{ID: msg.ID, Action: run.ActionResetResult, Payload: *c.result})
			}
			select {
			case c.msgs <- msg:
			default: // (always read: a pipe has no buffer, so the daemon would block)
			}
		}
	}()
	if err := bus.Write(run.Message{Action: run.ActionJoin, Payload: p}); err != nil {
		t.Fatal(err)
	}
	// (a restart from another instance can come before the ack)
	for c.next(t).Action != run.ActionJoinAck {
	}
	return c
}

func (c *client) next(t *testing.T) run.Message {
	t.Helper()
	select {
	case msg := <-c.msgs:
		return msg
	case <-time.After(time.Second):
		t.Fatal("no message from the daemon")
		return run.Message{}
	}
}

// eventually waits for the daemon to catch up w/ a disconnect
func eventually(t *testing.T, cond func() bool) {
	t.Helper()
	for range 100 {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never met")
}

func listed(s *Server, dir string) int {
	return len(s.targets.list(func(d string) bool { return d == dir }))
}

func TestSameCommandTwice(t *testing.T) {
	s := newServer()
	p := run.Process{Dir: "/site", Script: "rest", Args: []string{"-s"}}
	a := join(t, s, p)
	b := join(t, s, p)
	if n := listed(s, "/site"); n != 2 {
		t.Fatalf("listed %d, want both", n)
	}

	// a restart reaches both, and returns how each went
	results, err := s.targets.restart(p)
	if err != nil || len(results) != 2 {
		t.Fatalf("restart: %v %v", results, err)
	}
	for _, r := range results {
		if !r.OK || !r.Running || r.Output != running.Output {
			t.Errorf("result: %+v", r)
		}
	}
	for _, c := range []*client{a, b} {
		if msg := c.next(t); msg.Action != run.ActionReset {
			t.Fatalf("expected reset, got %v", msg.Action)
		}
	}

	// the first quitting leaves the second registered
	a.conn.Close()
	eventually(t, func() bool { return listed(s, "/site") == 1 })
	if results, err := s.targets.restart(p); err != nil || len(results) != 1 {
		t.Fatalf("restart after a left: %v %v", results, err)
	}

	// an explicit leave works too, and the empty directory is dropped
	b.next(t)
	b.bus.Write(run.Message{Action: run.ActionLeave})
	eventually(t, func() bool { return listed(s, "/site") == 0 })
	s.targets.mu.Lock()
	_, ok := s.targets.byDir["/site"]
	s.targets.mu.Unlock()
	if ok {
		t.Error("empty directory kept")
	}
	if _, err := s.targets.restart(p); err != errNotFound {
		t.Errorf("restart w/ nothing running: %v", err)
	}
	b.conn.Close()
}

func TestRejoinReplaces(t *testing.T) {
	s := newServer()
	c := join(t, s, run.Process{Dir: "/a", Script: "x"})
	c.bus.Write(run.Message{Action: run.ActionJoin, Payload: run.Process{Dir: "/b", Script: "x"}})
	c.next(t)
	if listed(s, "/a") != 0 || listed(s, "/b") != 1 {
		t.Errorf("a: %d b: %d", listed(s, "/a"), listed(s, "/b"))
	}
	c.conn.Close()
}

// many instances joining, restarting and leaving at once (run w/ -race)
func TestConcurrent(t *testing.T) {
	s := newServer()
	p := run.Process{Dir: "/site", Script: "rest"}
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c := join(t, s, p)
			s.targets.list(func(string) bool { return true })
			s.targets.restart(p)
			c.conn.Close()
		}()
	}
	wg.Wait()
	eventually(t, func() bool { return listed(s, "/site") == 0 })
}

func TestRestartResults(t *testing.T) {
	old := resetTimeout
	resetTimeout = 100 * time.Millisecond
	defer func() { resetTimeout = old }()

	s := newServer()
	crashed := &run.ResetResult{ExitCode: 1, Error: "exited w/ exit status 1", Output: "parse error\n"}
	cases := []struct {
		name   string
		result *run.ResetResult
		check  func(run.ResetResult) bool
	}{
		{"crashed", crashed, func(r run.ResetResult) bool { return !r.OK && r.ExitCode == 1 && r.Output == "parse error\n" }},
		{"no reply", nil, func(r run.ResetResult) bool { return !r.OK && strings.Contains(r.Error, "didn't confirm") }},
	}
	for _, tc := range cases {
		p := run.Process{Dir: "/" + tc.name, Script: "rest"}
		c := joinWith(t, s, p, tc.result)
		results, err := s.targets.restart(p)
		if err != nil || len(results) != 1 || !tc.check(results[0]) {
			t.Errorf("%s: %+v %v", tc.name, results, err)
		}
		c.conn.Close()
	}

	// run exiting mid restart answers right away
	p := run.Process{Dir: "/exits", Script: "rest"}
	c := joinWith(t, s, p, nil)
	resetTimeout = 5 * time.Second
	go func() {
		<-c.msgs // the reset
		c.conn.Close()
	}()
	start := time.Now()
	results, _ := s.targets.restart(p)
	if len(results) != 1 || !strings.Contains(results[0].Error, "exited during") || time.Since(start) > time.Second {
		t.Errorf("exited: %+v after %v", results, time.Since(start))
	}

	if text := describeRestart("rest", []run.ResetResult{*running, *crashed}); !strings.Contains(text, "it's running") ||
		!strings.Contains(text, "failed: exited w/ exit status 1\noutput since the restart:\nparse error") {
		t.Errorf("describe:\n%s", text)
	}
}
