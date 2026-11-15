package run

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Scripts can handle file changes themselves instead of being restarted.
// run starts every script w/ a socket on fd 3 (and RUN_EVENTS_FD=3), which
// a script that doesn't know about it never touches. One that does writes
// lines to it:
//
//	hello     handle -w changes myself (instead of a restart)
//	restart   restart me (ex. for a change it can't handle)
//
// and after hello, reads a batch of changes per settled burst of saves:
//
//	change added public/img/new.png
//	change modified src/docs/intro.md
//	change removed src/old.mn
//	done
//
// r and daemon restarts are still restarts.

// EventsFD is the fd scripts get the events socket on
const EventsFD = 3

// eventsWriteTimeout is how long run waits on a script that said hello but
// isn't reading, before restarting it instead
var eventsWriteTimeout = time.Second

// events is run's end of a script's fd 3
type events struct {
	f     *os.File
	hello atomic.Bool
}

// newEvents makes the socket pair, the script's end goes in ExtraFiles
func newEvents() (*events, *os.File, error) {
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		return nil, nil, err
	}
	syscall.CloseOnExec(fds[0])
	// (non-blocking, so writes can time out)
	if err := syscall.SetNonblock(fds[0], true); err != nil {
		syscall.Close(fds[0])
		syscall.Close(fds[1])
		return nil, nil, err
	}
	return &events{f: os.NewFile(uintptr(fds[0]), "run-events")},
		os.NewFile(uintptr(fds[1]), "run-events-script"), nil
}

// read handles what the script writes until it closes its end (or exits)
func (e *events) read(restartch chan<- bool) {
	sc := bufio.NewScanner(e.f)
	for sc.Scan() {
		switch strings.TrimSpace(sc.Text()) {
		case "hello":
			e.hello.Store(true)
		case "restart":
			restartch <- true
		}
	}
}

// send writes a batch, false if the script hasn't said hello or doesn't
// read it in time (then it should be restarted instead)
func (e *events) send(changes []Change) bool {
	if e == nil || !e.hello.Load() {
		return false
	}
	var b strings.Builder
	for _, c := range changes {
		if strings.ContainsAny(c.Path, "\n\r") {
			continue // (can't be a line)
		}
		fmt.Fprintf(&b, "change %s %s\n", c.Kind, c.Path)
	}
	b.WriteString("done\n")
	e.f.SetWriteDeadline(time.Now().Add(eventsWriteTimeout))
	_, err := e.f.WriteString(b.String())
	return err == nil
}

func (e *events) close() {
	if e != nil {
		e.f.Close()
	}
}
