package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestServiceFile(t *testing.T) {
	svc, err := newService("/home/me/go/bin/run-daemon")
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		if err == nil {
			t.Error("expected unsupported")
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	switch runtime.GOOS {
	case "darwin":
		if !strings.Contains(svc.content, "<string>/home/me/go/bin/run-daemon</string>") ||
			!strings.HasSuffix(svc.file, "Library/LaunchAgents/com.taybart.run-daemon.plist") {
			t.Errorf("%s:\n%s", svc.file, svc.content)
		}
		// it's a valid plist
		p := filepath.Join(t.TempDir(), "x.plist")
		os.WriteFile(p, []byte(svc.content), 0o644)
		if out, err := exec.Command("plutil", "-lint", p).CombinedOutput(); err != nil {
			t.Errorf("plutil: %v %s", err, out)
		}
	case "linux":
		if !strings.Contains(svc.content, "ExecStart=/home/me/go/bin/run-daemon\n") ||
			!strings.HasSuffix(svc.file, ".config/systemd/user/run-daemon.service") {
			t.Errorf("%s:\n%s", svc.file, svc.content)
		}
	}
}

func TestPlistEscapes(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip()
	}
	svc, _ := newService("/a & b/<run-daemon>")
	if !strings.Contains(svc.content, "/a &amp; b/&lt;run-daemon&gt;") {
		t.Error(svc.content)
	}
}

func TestCheckExecutable(t *testing.T) {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	for exe, ok := range map[string]bool{
		"/Users/me/.go/bin/run-daemon":                       true,
		"/usr/local/bin/run-daemon":                          true,
		"/private/var/folders/x/go-build123/b001/exe/daemon": false,
		filepath.Join(tmp, "run-daemon"):                     false,
	} {
		if err := checkExecutable(exe); (err == nil) != ok {
			t.Errorf("%s: %v", exe, err)
		}
	}
}
