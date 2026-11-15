package run

import (
	"net"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnedByMe(t *testing.T) {
	// a socket the current user made is owned by them
	l, err := net.Listen("unix", filepath.Join(t.TempDir(), "s.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	if !OwnedByMe(l.Addr().String()) {
		t.Error("socket should be owned by the current user")
	}

	// a regular file isn't a socket
	p := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(p, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	if OwnedByMe(p) {
		t.Error("regular file shouldn't count as a socket")
	}

	// nothing there
	if OwnedByMe(filepath.Join(t.TempDir(), "nope.sock")) {
		t.Error("missing path shouldn't count")
	}
}

func TestSocketPathNotInTmp(t *testing.T) {
	// the socket moved out of the shared /tmp so another user can't squat it
	dir := filepath.Dir(SocketPath())
	if dir == "/tmp" || dir == "/var/tmp" {
		t.Errorf("socket is in a shared temp dir: %s", SocketPath())
	}
}
