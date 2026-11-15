package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// run-daemon install/uninstall make it a per-user service: a launchd agent
// on macos, a systemd user unit on linux. Both start it at login and restart
// it if it dies. install is also how to update: it rewrites the service file
// and restarts it, so the binary it runs is the one just installed.

const serviceLabel = "com.taybart.run-daemon"

var plistTmpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>Label</key><string>%[1]s</string>
  <key>ProgramArguments</key>
  <array><string>%[2]s</string></array>
  <key>RunAtLoad</key><true/>
  <key>KeepAlive</key><true/>
  <key>StandardOutPath</key><string>%[3]s</string>
  <key>StandardErrorPath</key><string>%[3]s</string>
</dict>
</plist>
`

var unitTmpl = `[Unit]
Description=run-daemon

[Service]
ExecStart=%s
Restart=on-failure

[Install]
WantedBy=default.target
`

type service struct {
	file    string // where the service file goes
	content string // what's in it
	install [][]string
	remove  [][]string
	loaded  []string // succeeds if the service is loaded (installed and on)
	logs    string   // how to see its logs
}

func newService(exe string) (*service, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	switch runtime.GOOS {
	case "darwin":
		domain := fmt.Sprintf("gui/%d", os.Getuid())
		file := filepath.Join(home, "Library", "LaunchAgents", serviceLabel+".plist")
		logFile := filepath.Join(home, "Library", "Logs", "run-daemon.log")
		return &service{
			file:    file,
			content: fmt.Sprintf(plistTmpl, serviceLabel, xmlEscape(exe), xmlEscape(logFile)),
			// (bootout first, so a reinstall picks up the new file and binary)
			install: [][]string{{"launchctl", "bootout", domain + "/" + serviceLabel}, {"launchctl", "bootstrap", domain, file}},
			remove:  [][]string{{"launchctl", "bootout", domain + "/" + serviceLabel}},
			loaded:  []string{"launchctl", "print", domain + "/" + serviceLabel},
			logs:    "tail -f " + logFile,
		}, nil
	case "linux":
		return &service{
			file:    filepath.Join(home, ".config", "systemd", "user", "run-daemon.service"),
			content: fmt.Sprintf(unitTmpl, exe),
			install: [][]string{
				{"systemctl", "--user", "daemon-reload"},
				{"systemctl", "--user", "enable", "run-daemon"},
				{"systemctl", "--user", "restart", "run-daemon"},
			},
			remove: [][]string{{"systemctl", "--user", "disable", "--now", "run-daemon"}},
			loaded: []string{"systemctl", "--user", "is-active", "--quiet", "run-daemon"},
			logs:   "journalctl --user -u run-daemon -f",
		}, nil
	}
	return nil, fmt.Errorf("install isn't supported on %s", runtime.GOOS)
}

func xmlEscape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// executable is the path of this binary, which the service runs
func executable() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return "", err
	}
	return exe, checkExecutable(exe)
}

// checkExecutable refuses binaries that won't be there later (ex. go run's)
func checkExecutable(exe string) error {
	tmp, _ := filepath.EvalSymlinks(os.TempDir())
	if strings.Contains(exe, string(filepath.Separator)+"go-build") ||
		(tmp != "" && strings.HasPrefix(exe, tmp+string(filepath.Separator))) {
		return fmt.Errorf("%s is a temporary binary (go run?), install it first (ex. go install ./cmd/run-daemon) and run that", exe)
	}
	return nil
}

func installService() error {
	exe, err := executable()
	if err != nil {
		return err
	}
	svc, err := newService(exe)
	if err != nil {
		return err
	}
	// a run-daemon that isn't the service (ex. run-daemon &) would keep the
	// service from starting
	if daemonRunning() && exec.Command(svc.loaded[0], svc.loaded[1:]...).Run() != nil {
		return errors.New("a run-daemon that isn't the service is running (started by hand?), stop it first")
	}
	if err := os.MkdirAll(filepath.Dir(svc.file), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(svc.file, []byte(svc.content), 0o644); err != nil {
		return err
	}
	fmt.Println("wrote", svc.file)

	for i, args := range svc.install {
		out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
		if err == nil {
			continue
		}
		if runtime.GOOS == "darwin" && i == 0 {
			continue // bootout fails when it wasn't loaded yet, that's fine
		}
		if runtime.GOOS == "darwin" {
			// bootout returns before the agent is fully gone, bootstrap
			// fails until it is
			for range 20 {
				time.Sleep(250 * time.Millisecond)
				if out, err = exec.Command(args[0], args[1:]...).CombinedOutput(); err == nil {
					break
				}
			}
			if err == nil {
				continue
			}
		}
		return fmt.Errorf("%s: %v\n%s", strings.Join(args, " "), err, out)
	}

	// it's running once it answers (another run-daemon, ex. one started by
	// hand, would keep it from starting)
	for range 20 {
		if daemonRunning() {
			fmt.Printf("run-daemon is running (%s), it starts at login\nlogs: %s\n", exe, svc.logs)
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("installed, but run-daemon didn't start. If one you started by hand is still running, stop it. logs: " + svc.logs)
}

func uninstallService() error {
	svc, err := newService("")
	if err != nil {
		return err
	}
	for _, args := range svc.remove {
		if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
			fmt.Printf("%s: %v %s\n", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
		}
	}
	if err := os.Remove(svc.file); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if runtime.GOOS == "linux" {
		exec.Command("systemctl", "--user", "daemon-reload").Run()
	}
	fmt.Println("removed", svc.file)
	return nil
}
