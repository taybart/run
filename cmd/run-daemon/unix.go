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

// handle is one run instance's connection, it's registered from its join
// until it leaves or the connection closes (ex. run exits or is killed)
func (s *Server) handle(conn net.Conn) {
	defer conn.Close()
	bus, err := run.NewBus(conn)
	if err != nil {
		fmt.Println(err)
		return
	}
	var me *Target
	var dir string
	leave := func() {
		if me != nil {
			fmt.Printf("leaving %s in directory %s\n", me.script, dir)
			s.targets.remove(dir, me)
			me = nil
		}
	}
	defer leave()

	for {
		msg, err := bus.Read()
		if err != nil {
			return
		}
		switch msg.Action {
		case run.ActionJoin:
			pl, ok := msg.Payload.(run.Process)
			if !ok {
				fmt.Println("join w/o a process")
				return
			}
			leave() // (a second join replaces the first)
			fmt.Printf("joining %s in directory %s\n", pl.Script, pl.Dir)
			dir = pl.Dir
			me = &Target{key: makeKey(pl), script: pl.Script, args: pl.Args, bus: bus}
			s.targets.add(dir, me)
			if err := me.send(run.Message{Action: run.ActionJoinAck}); err != nil {
				return
			}
		case run.ActionLeave:
			leave()
		default:
			fmt.Println("unknown or out of order action:", msg.Action)
		}
	}
}
