package main

import (
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"time"

	"github.com/eiannone/keyboard"
	"github.com/taybart/log"
)

func main() {
	log.SetPlain()
	if len(os.Args) < 2 {
		fmt.Printf("%sUsage: program <script-to-run> [args...]%s\n", log.Red, log.Rtd)
		os.Exit(1)
	}

	script := os.Args[1]
	scriptArgs := os.Args[2:]

	// Create channels for communication
	quitChan := make(chan bool)
	reloadChan := make(chan bool)
	doneChan := make(chan bool)

	// Handle Ctrl+C gracefully
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigChan
		log.Info("\nReceived interrupt, cleaning up...")
		keyboard.Close()
		os.Exit(0)
	}()

	// Start keyboard handler - this runs continuously throughout the program
	go func() {
		err := keyboard.Open()
		if err != nil {
			log.Errorf("Error opening keyboard: %v\n", err)
			os.Exit(1)
		}
		defer keyboard.Close()

		log.Infof("%sPress 'r' to reload, 'q' to quit%s\n", log.Blue, log.Rtd)

		for {
			char, key, err := keyboard.GetKey()
			if err != nil {
				log.Error("Error reading keyboard: %v\n", err)
				break
			}

			if char == 'q' || key == keyboard.KeyEsc {
				quitChan <- true
				return
			} else if char == 'r' {
				reloadChan <- true
			}
		}
	}()

	log.Infof("Running: %s\n", script)

	var cmd *exec.Cmd
	cmd = run(script, scriptArgs, doneChan)

	for {
		select {
		case <-quitChan:
			log.Infof("\n%sExiting...%s\n", log.Green, log.Rtd)
			if cmd != nil && cmd.Process != nil {
				kill(cmd)
			}
			return
		case <-reloadChan:
			log.Infof("%s\nReloading script...%s\n", log.Green, log.Rtd)
			if cmd != nil && cmd.Process != nil {
				kill(cmd)
			}
			// Small delay to ensure process termination
			time.Sleep(500 * time.Millisecond)
			cmd = run(script, scriptArgs, doneChan)
		case <-doneChan:
			log.Debug("Script execution completed")
		}
	}
}

func run(script string, args []string, doneChan chan<- bool) *exec.Cmd {
	cmd := exec.Command(script, args...)

	// Set process group for better process management
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true, // Create a new process group
	}

	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		log.Error("Could not start script: %v\n", err)
		if doneChan != nil {
			doneChan <- true
		}
		return nil
	}

	go func() {
		cmd.Wait()
		if doneChan != nil {
			doneChan <- true
		}
	}()

	return cmd
}

func kill(cmd *exec.Cmd) {
	if cmd.Process == nil {
		log.Warn("no process")
		return
	}

	log.Infof("Terminating process...%d\n", cmd.Process.Pid)

	// On Unix-like systems, use process groups to kill all children
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
		log.Debugf("using unix-like termination on pid: %d\n", pgid)
		syscall.Kill(-pgid, syscall.SIGTERM)
		time.Sleep(200 * time.Millisecond)
		syscall.Kill(-pgid, syscall.SIGKILL)
	} else {
		log.Debugf("%susing fallback go kill method%s\n", log.Red, log.Rtd)
		// Fallback if process group not available
		cmd.Process.Signal(syscall.SIGTERM)
		time.Sleep(200 * time.Millisecond)
		cmd.Process.Kill()
	}
}
