package main

//run-daemon, holds list of running processes
//run with default action, register to the daemon if available fifo pipe in tmp or something?

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/taybart/run"
)

func main() {
	sysInterrupt()
	s := newServer()
	if err := s.Listen(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

type Target struct {
	script string
	args   []string
	bus    *run.Bus
}

type Server struct {
	targets map[string]map[string]*Target
}

func newServer() *Server {
	return &Server{
		targets: make(map[string]map[string]*Target),
	}
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

func sysInterrupt() {
	// Cleanup the sockfile.
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		os.Remove(run.SocketPath)
		os.Exit(1)
	}()
}
