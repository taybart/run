package run

import (
	"encoding/gob"
	"net"
)

/***
* Server:
* 1. listens on socket for client messages that register directory and port its on
* 2.
* 3.
***/
/***
* MCP:
* 1. mcp asks about folder x /home/user/project
* 2. we return a list of running processes from that top level down
* 3. mcp can restart service x
***/

func init() {
	gob.Register(Process{})
}

const (
	SocketPath = "/tmp/run-daemon.sock"
)

type Action int

const (
	ActionJoin Action = iota
	ActionJoinAck
	ActionReset
	ActionLeave
)

type Message struct {
	ID      int
	Action  Action
	Payload any
}

type Process struct {
	Dir    string   `json:"dir,omitempty"`
	Script string   `json:"script,omitempty"`
	Args   []string `json:"args,omitempty"`
}

func (a Action) String() string {
	switch a {
	case ActionJoin:
		return "join"
	case ActionJoinAck:
		return "join_ack"
	case ActionReset:
		return "reset"
	case ActionLeave:
		return "leave"
	default:
		return "unknown"
	}
}

type Bus struct {
	conn net.Conn
	dec  *gob.Decoder
	enc  *gob.Encoder
}

func NewBus(conn net.Conn) (*Bus, error) {
	if conn == nil {
		var err error
		conn, err = net.Dial("unix", SocketPath)
		if err != nil {
			return nil, err
		}
	}
	return &Bus{conn, gob.NewDecoder(conn), gob.NewEncoder(conn)}, nil
}

func (b *Bus) Write(msg Message) error {
	if err := b.enc.Encode(msg); err != nil {
		return err
	}
	return nil
}
func (b *Bus) Read() (Message, error) {
	var msg Message
	if err := b.dec.Decode(&msg); err != nil {
		return msg, err
	}
	return msg, nil
}
func (b *Bus) Close() error {
	return b.conn.Close()
}
