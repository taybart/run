package run

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// a script that says hello on fd 3 gets the changes, w/ a done per batch
func TestEventsHello(t *testing.T) {
	dir := t.TempDir()
	got := filepath.Join(dir, "got")
	script := filepath.Join(dir, "hot.sh")
	os.WriteFile(script, []byte(`#!/bin/sh
[ "$RUN_EVENTS_FD" = 3 ] || exit 2
echo hello >&3
while read -r line <&3; do
  echo "$line" >> "`+got+`"
  [ "$line" = done ] && echo restart >&3
done
`), 0o755)

	restartch := make(chan bool, 1)
	p, err := run(script, nil, io.Discard, restartch)
	if err != nil {
		t.Fatal(err)
	}
	defer kill(p.cmd)

	for range 100 { // (wait for hello)
		if p.events.hello.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !p.events.send([]Change{{"modified", "docs/a b.md"}, {"added", "x.mn"}, {"added", "bad\nname"}}) {
		t.Fatal("send failed")
	}
	// it asks for a restart after the batch
	select {
	case <-restartch:
	case <-time.After(time.Second):
		t.Fatal("no restart request")
	}
	b, _ := os.ReadFile(got)
	if want := "change modified docs/a b.md\nchange added x.mn\ndone\n"; string(b) != want {
		t.Errorf("got %q, want %q", b, want)
	}
}

// a script that doesn't know about fd 3 is restarted instead (send says no)
func TestEventsNoHello(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "plain.sh")
	os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 5\n"), 0o755)
	p, err := run(script, nil, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer kill(p.cmd)
	time.Sleep(50 * time.Millisecond)
	if p.events.send([]Change{{"modified", "a.mn"}}) {
		t.Error("sent to a script that didn't say hello")
	}
}

// a script that says hello but stops reading gets restarted, run doesn't hang
func TestEventsNotReading(t *testing.T) {
	old := eventsWriteTimeout
	eventsWriteTimeout = 100 * time.Millisecond
	defer func() { eventsWriteTimeout = old }()

	dir := t.TempDir()
	script := filepath.Join(dir, "stuck.sh")
	os.WriteFile(script, []byte("#!/bin/sh\necho hello >&3\nexec sleep 5\n"), 0o755)
	p, err := run(script, nil, io.Discard, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer kill(p.cmd)
	for range 100 {
		if p.events.hello.Load() {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	big := []Change{{"modified", strings.Repeat("x", 4000)}}
	start := time.Now()
	for range 1000 { // (more than the socket buffer)
		if !p.events.send(big) {
			if time.Since(start) > 2*time.Second {
				t.Errorf("took %v to give up", time.Since(start))
			}
			return
		}
	}
	t.Error("never gave up on a script that doesn't read")
}
