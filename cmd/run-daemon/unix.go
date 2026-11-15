package main

import (
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"

	"github.com/taybart/run"
)

func (s *Server) listenUnix() error {
	path := run.SocketPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	os.Remove(path) // clear stale socket
	defer os.Remove(path)

	l, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	if err := os.Chmod(path, 0o600); err != nil { // only our user can connect
		l.Close()
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
			close(me.gone)
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
				fmt.Println("join without a process")
				return
			}
			leave() // (a second join replaces the first)
			fmt.Printf("joining %s in directory %s\n", pl.Script, pl.Dir)
			dir = pl.Dir
			me = newTarget(pl, bus)
			s.targets.add(dir, me)
			if err := me.send(run.Message{Action: run.ActionJoinAck}); err != nil {
				return
			}
		case run.ActionLeave:
			leave()
		case run.ActionResetResult:
			res, ok := msg.Payload.(run.ResetResult)
			if me != nil && ok {
				me.replied(msg.ID, res)
			}
		case run.ActionLogsResult:
			res, ok := msg.Payload.(run.LogsResult)
			if me != nil && ok {
				me.repliedLogs(msg.ID, res)
			}
		default:
			fmt.Println("unknown or out of order action:", msg.Action)
		}
	}
}
