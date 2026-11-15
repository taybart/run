package main

//run-daemon, holds list of running processes
//run with default action, register to the daemon if available fifo pipe in tmp or something?

import (
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/taybart/run"
)

func usage() {
	fmt.Println(`Usage: run-daemon [install | uninstall]
  (none)     run the daemon
  install    install (or update) it as a service that starts at login, and start it
  uninstall  stop it and remove the service`)
}

func main() {
	if len(os.Args) > 1 {
		var err error
		switch os.Args[1] {
		case "install":
			err = installService()
		case "uninstall":
			err = uninstallService()
		default:
			usage()
			os.Exit(1)
		}
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		return
	}
	// another one would take its socket, then fail on the http port
	if daemonRunning() {
		fmt.Println("run-daemon is already running")
		os.Exit(1)
	}
	sysInterrupt()
	s := newServer()
	if err := s.Listen(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type Server struct {
	targets *targets
}

func newServer() *Server {
	return &Server{targets: newTargets()}
}

func (s *Server) Listen() error {
	go func() {
		if err := s.listenUnix(); err != nil {
			log.Fatal(err)
		}
	}()
	// TODO: register/serve mcp routes
	return s.listenHTTP()
}

func makeKey(pl run.Process) string {
	// h := sha256.New()
	// h.Write([]byte(pl.Script))
	// for _, a := range pl.Args {
	// 	h.Write([]byte(a))
	// }
	// return base64.StdEncoding.EncodeToString(h.Sum(nil))
	return strings.Join(append([]string{pl.Script}, pl.Args...), "\x00")
}

// daemonRunning is if a run-daemon answers on the socket (and it's our socket,
// not one another user left there)
func daemonRunning() bool {
	if !run.OwnedByMe(run.SocketPath()) {
		return false
	}
	conn, err := net.DialTimeout("unix", run.SocketPath(), time.Second)
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func sysInterrupt() {
	// Cleanup the sockfile.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Remove(run.SocketPath())
		os.Exit(1)
	}()
}
