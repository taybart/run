package run

import (
	"encoding/gob"
	"net"
	"sync"
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
	gob.Register(ResetResult{})
	gob.Register(LogsResult{})
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
	// ActionResetResult is run's reply to a reset (w/ the reset's ID)
	ActionResetResult
	// ActionLogs asks run for the tail of the script's output, and
	// ActionLogsResult is its reply (w/ the request's ID)
	ActionLogs
	ActionLogsResult
)

type Message struct {
	ID      int
	Action  Action
	Payload any
}

// ResetResult is how a restart went: the script is still running after
// ResetSettle, or it exited (OK if w/ 0), and what it printed since the restart
type ResetResult struct {
	OK        bool   `json:"ok"`
	Running   bool   `json:"running"`
	ExitCode  int    `json:"exit_code,omitempty"`
	Error     string `json:"error,omitempty"`
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
}

// LogsResult is the tail of what the script printed (stdout and stderr), the
// reply to a logs request
type LogsResult struct {
	Output    string `json:"output"`
	Truncated bool   `json:"truncated,omitempty"`
	Error     string `json:"error,omitempty"`
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
	case ActionResetResult:
		return "reset_result"
	case ActionLogs:
		return "logs"
	case ActionLogsResult:
		return "logs_result"
	default:
		return "unknown"
	}
}

// Bus is a connection between run and run-daemon, safe to write from
// several goroutines (one reader)
type Bus struct {
	conn net.Conn
	dec  *gob.Decoder
	mu   sync.Mutex
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
	return &Bus{conn: conn, dec: gob.NewDecoder(conn), enc: gob.NewEncoder(conn)}, nil
}

func (b *Bus) Write(msg Message) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.enc.Encode(msg)
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
