package main

import (
	"fmt"
	"log"
	"net"
	"os"

	"github.com/taybart/run"
)

func (s *Server) listenUnix() error {
	os.Remove(run.SocketPath) // clear stale socket
	defer os.Remove(run.SocketPath)

	l, err := net.Listen("unix", run.SocketPath)
	if err != nil {
		return err
	}
	defer l.Close()

	log.Println("listening for run instances on", l.Addr())
	for {
		conn, err := l.Accept()
		if err != nil {
			return err
		}
		go s.handle(conn)
	}
}
func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	bus, err := run.NewBus(conn)
	if err != nil {
		fmt.Println(err)
		return
	}
	var key string
	var dir string
	for {
		msg, err := bus.Read()
		if err != nil {
			if key != "" { // are we previously connected?
				delete(s.targets[dir], key)
			}
			return
		}
		switch msg.Action {
		case run.ActionJoin:
			pl := msg.Payload.(run.Process)
			fmt.Printf("joining %s in directory %s\n", pl.Script, pl.Dir)
			if s.targets[pl.Dir] == nil {
				s.targets[pl.Dir] = make(map[string]*Target)
			}
			bus, err := run.NewBus(conn)
			if err != nil {
				fmt.Println(err)
				return
			}
			dir = pl.Dir
			key = makeKey(pl)
			s.targets[pl.Dir][key] = &Target{
				script: pl.Script,
				args:   pl.Args,
				bus:    bus,
			}
			if err := bus.Write(run.Message{Action: run.ActionJoinAck}); err != nil {
				return
			}
		case run.ActionLeave:
			if dir != "" && key != "" { // are we previously connected?
				delete(s.targets[dir], key)
			}
		default:
			fmt.Println("unknown or out of order action:", msg.Action)
		}
	}
}
