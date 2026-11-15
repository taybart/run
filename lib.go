package run

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/eiannone/keyboard"
	"github.com/taybart/log"
)

// ResetSettle is how long after a daemon restart run waits to see if the
// script keeps running, before it reports back
const ResetSettle = time.Second

// outputLimit is the most output a restart reports (the end of it)
const outputLimit = 4 << 10

// Do runs script, restarting it on r, a daemon restart, or (w/ watch, a list
// of file extensions or "*") when files in the working directory change.
func Do(script string, scriptArgs []string, tryDaemon bool, watchExts []string) {
	// communication channels
	quitch := make(chan bool)
	reconnch := make(chan bool)
	reloadch := make(chan bool)
	resetch := make(chan int) // daemon restarts, by id (to reply to)

	// signals (ex. kill, a closed terminal) quit like q: the script is in its
	// own process group, so w/o this it would keep running after run exits
	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sigch
		quitch <- true
	}()

	// start keyboard handler
	var quitting atomic.Bool
	go func() {
		if err := keyboard.Open(); err != nil {
			log.Errorf("Error opening keyboard: %v\n", err)
			os.Exit(1)
		}
		defer keyboard.Close()

		for {
			char, key, err := keyboard.GetKey()
			if err != nil {
				if !quitting.Load() { // (closing the keyboard cancels GetKey)
					log.Errorf("Error reading keyboard: %v\n", err)
				}
				break
			}

			if key == keyboard.KeyEnter {
				fmt.Println()
			}
			if char == 'q' || key == keyboard.KeyEsc || key == keyboard.KeyCtrlC {
				quitch <- true
				return
			} else if char == 'c' {
				reconnch <- true
			} else if char == 'r' {
				reloadch <- true
			}
		}
	}()

	// one connection to the daemon at a time, c replaces it
	var daemon *Bus
	joinDaemon := func() {
		if daemon != nil {
			daemon.Close()
		}
		daemon = connect(quitch, resetch, script, scriptArgs)
	}
	if tryDaemon {
		joinDaemon()
	}

	if len(watchExts) > 0 {
		go watch(".", watchExts, pollInterval, reloadch)
	}

	out := newOutputLog(64 << 10)

	// start script
	p, err := run(script, scriptArgs, out)
	if err != nil {
		log.Errorf("could not start script: %s%v%s\n", log.Red, err, log.Reset)
		return
	}

	restart := func() (int64, error) {
		log.Infof("%s\nReloading script...%s\n", log.Green, log.Rtd)
		if p != nil {
			kill(p.cmd)
		}
		// Small delay to ensure process termination
		time.Sleep(500 * time.Millisecond)
		mark := out.mark()
		var err error
		if p, err = run(script, scriptArgs, out); err != nil {
			log.Errorf("could not start script: %s%v%s\n", log.Red, err, log.Reset)
		}
		return mark, err
	}

	for {
		select {
		// q/esc/ctrl+c hit
		case <-quitch:
			log.Infof("\n%sExiting...%s\n", log.Green, log.Rtd)
			if p != nil {
				kill(p.cmd)
			}
			if daemon != nil {
				daemon.Close() // (leaves right away)
			}
			quitting.Store(true)
			keyboard.Close() // restore the terminal
			return
		case <-reconnch:
			if tryDaemon {
				log.Infof("\n%sReconnecting...%s\n", log.Green, log.Rtd)
				joinDaemon()
			}
		// r hit (or a watched file changed)
		case <-reloadch:
			restart()
		case id := <-resetch:
			mark, err := restart()
			go report(daemon, id, p, err, out, mark)
		}
	}
}

// proc is one run of the script
type proc struct {
	cmd  *exec.Cmd
	done chan struct{} // closed when it exits, w/ err set
	err  error
}

func run(script string, args []string, out io.Writer) (*proc, error) {
	cmd := exec.Command(script, args...)

	// Set process group for better process management
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create a new process group
	}

	cmd.Stdout = io.MultiWriter(os.Stdout, out)
	cmd.Stderr = io.MultiWriter(os.Stderr, out)

	if err := cmd.Start(); err != nil {
		return nil, err
	}
	p := &proc{cmd: cmd, done: make(chan struct{})}
	go func() {
		p.err = cmd.Wait()
		close(p.done)
	}()
	return p, nil
}

// report tells the daemon how restart id went: if the script is still
// running after ResetSettle (or how it exited), and what it printed.
func report(daemon *Bus, id int, p *proc, startErr error, out *outputLog, mark int64) {
	if daemon == nil || id == 0 { // (0: an older daemon, it doesn't wait)
		return
	}
	res := ResetResult{}
	if startErr != nil {
		res.Error = "could not start script: " + startErr.Error()
	} else {
		select {
		case <-p.done:
			res.ExitCode = p.cmd.ProcessState.ExitCode()
			res.OK = p.err == nil
			if !res.OK {
				res.Error = fmt.Sprintf("exited w/ %v", p.err)
			}
		case <-time.After(ResetSettle):
			res.OK, res.Running = true, true
		}
	}
	res.Output, res.Truncated = out.since(mark, outputLimit)
	daemon.Write(Message{ID: id, Action: ActionResetResult, Payload: res})
}

func kill(cmd *exec.Cmd) {
	if cmd.Process == nil {
		log.Warn("No process to terminate")
		return
	}

	log.Debugf("terminating process...%d\n", cmd.Process.Pid)

	// On Unix-like systems, use process groups to kill all children
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
		log.Debugf("using unix-like termination on pid: %d\n", pgid)
		syscall.Kill(-pgid, syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		// Fallback if process group not available
		log.Debugf("%susing fallback go kill method%s\n", log.Red, log.Rtd)
		cmd.Process.Signal(syscall.SIGTERM)
		time.Sleep(100 * time.Millisecond)
		cmd.Process.Kill()
	}
}

// connect joins run-daemon (nil if it isn't running) and listens to it in
// the background, closing the bus leaves
func connect(quitch chan bool, resetch chan int, script string, scriptArgs []string) *Bus {
	workingDir, err := os.Getwd()
	if err != nil {
		fmt.Printf("%sError getting working directory: %s%s\n", log.Red, err, log.Rtd)
		return nil
	}
	bus, err := NewBus(nil)
	if err != nil {
		fmt.Printf("%s...daemon unavailable...%s\n", log.ItalicYellow, log.Rtd)
		return nil
	}
	pl := Process{Dir: workingDir, Script: script, Args: scriptArgs}
	if err := bus.Write(Message{Action: ActionJoin, Payload: pl}); err != nil {
		fmt.Printf("%sError joining daemon: %s%s\n", log.Red, err, log.Rtd)
		bus.Close()
		return nil
	}
	go listen(bus, quitch, resetch)
	return bus
}

func listen(bus *Bus, quitch chan bool, resetch chan int) {
	for {
		msg, err := bus.Read()
		if err != nil {
			if err != io.EOF && !errors.Is(err, net.ErrClosed) {
				fmt.Printf("%sError: %s%s\n", log.Red, err, log.Rtd)
			}
			break
		}

		switch msg.Action {
		case ActionJoinAck:
			fmt.Printf("%sRegistered with daemon%s\n", log.ItalicGreen, log.Rtd)
		case ActionReset:
			resetch <- msg.ID
		case ActionLeave:
			quitch <- true
		}
	}
}
