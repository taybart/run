package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/eiannone/keyboard"
	"github.com/taybart/log"
)

func main() {
	log.SetPlain()
	if len(os.Args) < 2 {
		fmt.Printf("%sUsage: run <script-to-run> [args...]%s\n", log.Red, log.Rtd)
		os.Exit(1)
	}

	script := os.Args[1]
	scriptArgs := os.Args[2:]

	log.Infof("%sRunning %s...%s\n", log.Blue, script, log.Rtd)
	log.Infof("%sPress 'r' to reload, 'q' to quit%s\n", log.Yellow, log.Rtd)

	// communication channels
	quitch := make(chan bool)
	reloadch := make(chan bool)
	donech := make(chan bool)

	// start keyboard handler
	go func() {
		if err := keyboard.Open(); err != nil {
			log.Errorf("Error opening keyboard: %v\n", err)
			os.Exit(1)
		}
		defer keyboard.Close()

		for {
			char, key, err := keyboard.GetKey()
			if err != nil {
				log.Error("Error reading keyboard: %v\n", err)
				break
			}

			if key == keyboard.KeyEnter {
				fmt.Println()
			}
			if char == 'q' || key == keyboard.KeyEsc || key == keyboard.KeyCtrlC {
				quitch <- true
				return
			} else if char == 'r' {
				reloadch <- true
			}
		}
	}()

	// start script
	cmd := run(script, scriptArgs, donech)
	if cmd == nil {
		log.Fatal("Something went wrong")
	}

	for {
		select {
		// q/esc/ctrl+c hit
		case <-quitch:
			log.Infof("\n%sExiting...%s\n", log.Green, log.Rtd)
			if cmd != nil && cmd.Process != nil {
				kill(cmd)
			}
			return
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
		log.Error("Could not start script: %v\n", err)
		if donech != nil {
			donech <- true
		}
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
