package run

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestOutputLog(t *testing.T) {
	o := newOutputLog(10)
	o.Write([]byte("old"))
	m := o.mark()
	o.Write([]byte("new"))
	if got, cut := o.since(m, 100); got != "new" || cut {
		t.Errorf("since: %q %v", got, cut)
	}
	o.Write([]byte("0123456789")) // pushes everything before out of the buffer
	if got, cut := o.since(m, 100); got != "0123456789" || !cut {
		t.Errorf("dropped: %q %v", got, cut)
	}
	if got, cut := o.since(o.mark()-4, 2); got != "89" || !cut {
		t.Errorf("limit: %q %v", got, cut)
	}
}

func TestOutputLogTail(t *testing.T) {
	o := newOutputLog(8)
	o.Write([]byte("0123456789")) // drops "01", keeps "23456789"
	if got, cut := o.tail(); got != "23456789" || !cut {
		t.Errorf("tail: %q %v", got, cut)
	}
	o.Write([]byte("x")) // still the last 8: "3456789x"
	if got, cut := o.tail(); got != "3456789x" || !cut {
		t.Errorf("tail after more: %q %v", got, cut)
	}
	small := newOutputLog(8)
	small.Write([]byte("hi"))
	if got, cut := small.tail(); got != "hi" || cut {
		t.Errorf("tail under max: %q %v", got, cut)
	}
}

// report sends how a restart went, with the output since it
func TestReport(t *testing.T) {
	dir := t.TempDir()
	script := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cases := []struct {
		name, script string
		check        func(ResetResult) bool
	}{
		{"running", script("up.sh", "echo listening; exec sleep 5"), func(r ResetResult) bool {
			return r.OK && r.Running && r.Output == "listening\n"
		}},
		{"crashed", script("crash.sh", "echo 'parse error' >&2; exit 3"), func(r ResetResult) bool {
			return !r.OK && !r.Running && r.ExitCode == 3 && r.Output == "parse error\n"
		}},
		{"done", script("done.sh", "echo built"), func(r ResetResult) bool {
			return r.OK && !r.Running && r.ExitCode == 0 && r.Output == "built\n"
		}},
		{"missing", filepath.Join(dir, "nope.sh"), func(r ResetResult) bool {
			return !r.OK && strings.Contains(r.Error, "could not start")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			daemonSide, runSide := net.Pipe()
			defer daemonSide.Close()
			defer runSide.Close()
			daemon, _ := NewBus(daemonSide)
			bus, _ := NewBus(runSide)

			out := newOutputLog(1 << 10)
			out.Write([]byte("from before the restart\n"))
			mark := out.mark()
			p, err := run(tc.script, nil, out, nil)
			if p != nil {
				defer kill(p.cmd)
			}
			start := time.Now()
			go report(bus, 7, p, err, out, mark)

			msg, rerr := daemon.Read()
			if rerr != nil {
				t.Fatal(rerr)
			}
			res, _ := msg.Payload.(ResetResult)
			if msg.ID != 7 || msg.Action != ActionResetResult || !tc.check(res) {
				t.Errorf("%+v %+v", msg, res)
			}
			// only a script that keeps running waits out the settle time
			if waited := time.Since(start); tc.name != "running" && waited > ResetSettle/2 {
				t.Errorf("waited %v for a script that already exited", waited)
			}
		})
	}
}
