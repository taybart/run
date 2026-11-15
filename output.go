package run

import "sync"

// outputLog keeps the last max bytes the script wrote (stdout and stderr), so
// a restart can report what the script printed since it started
type outputLog struct {
	mu    sync.Mutex
	max   int
	buf   []byte
	total int64 // bytes ever written
}

func newOutputLog(max int) *outputLog {
	return &outputLog{max: max}
}

func (o *outputLog) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.buf = append(o.buf, p...)
	if len(o.buf) > o.max {
		o.buf = append(o.buf[:0], o.buf[len(o.buf)-o.max:]...)
	}
	o.total += int64(len(p))
	return len(p), nil
}

// mark is where the output is now, for since
func (o *outputLog) mark() int64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.total
}

// since returns what was written after mark, at most limit bytes (the end),
// and whether it was cut short
func (o *outputLog) since(mark int64, limit int) (string, bool) {
	o.mu.Lock()
	defer o.mu.Unlock()
	start := int(mark - (o.total - int64(len(o.buf))))
	truncated := false
	if start < 0 { // (already dropped from the buffer)
		start, truncated = 0, true
	}
	out := o.buf[start:]
	if len(out) > limit {
		out, truncated = out[len(out)-limit:], true
	}
	return string(out), truncated
}
