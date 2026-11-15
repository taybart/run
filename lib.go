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

// Do runs script, restarting it on r, a daemon restart, or (w/ watch, a list
// of file extensions or "*") when files in the working directory change.
func Do(script string, scriptArgs []string, tryDaemon bool, watchExts []string) {
	// communication channels
	quitch := make(chan bool)
	reconnch := make(chan bool)
	reloadch := make(chan bool)
	donech := make(chan bool)

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
		daemon = connect(quitch, reloadch, script, scriptArgs)
	}
	if tryDaemon {
		joinDaemon()
	}

	if len(watchExts) > 0 {
		go watch(".", watchExts, pollInterval, reloadch)
	}

	// start script
	cmd := run(script, scriptArgs, donech)
	if cmd == nil {
		return
	}

	for {
		select {
		// q/esc/ctrl+c hit
		case <-quitch:
			log.Infof("\n%sExiting...%s\n", log.Green, log.Rtd)
			if cmd != nil && cmd.Process != nil {
				kill(cmd)
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
		// r hit
		case <-reloadch:
			log.Infof("%s\nReloading script...%s\n", log.Green, log.Rtd)
			if cmd != nil && cmd.Process != nil {
				kill(cmd)
			}
			// Small delay to ensure process termination
			time.Sleep(500 * time.Millisecond)
			cmd = run(script, scriptArgs, donech)
			if cmd == nil {
				log.Fatal("Something went wrong")
			}
		case <-donech:
			log.Debug("Script execution completed")
		}
	}
}

func run(script string, args []string, donech chan<- bool) *exec.Cmd {
	cmd := exec.Command(script, args...)

	// Set process group for better process management
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create a new process group
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Errorf("could not start script: %s%v%s\n", log.Red, err, log.Reset)
		return nil
	}

	go func() {
		cmd.Wait()
		if donech != nil {
			donech <- true
		}
	}()

	return cmd
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
func connect(quitch, reloadch chan bool, script string, scriptArgs []string) *Bus {
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
	go listen(bus, quitch, reloadch)
	return bus
}

func listen(bus *Bus, quitch, reloadch chan bool) {
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
			reloadch <- true
		case ActionLeave:
			quitch <- true
		}
	}
}
