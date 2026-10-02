// Package rpc implements the newline-delimited JSON protocol spoken between
// ervisiod and ervisio-bridge: the bridge-side Registry and Serve loop,
// and the daemon-side Client.
//
// Wire format (one JSON object per line):
//
//	← {"hello":{"version":"…","uid":1000,"admin":false,"methods":{"x.y":"user",…}}}   first line
//	→ {"id":1,"method":"services.list","params":{}}
//	← {"id":1,"result":…} | {"id":1,"error":{"code":"…","message":"…"}}
//	→ {"id":2,"method":"logs.follow","params":{…},"stream":true}
//	← {"id":2,"event":"data","data":…[,"b64":true]} … {"id":2,"event":"end"} | {"id":2,"error":{…}}
//	→ {"id":2,"input":…}   → {"id":2,"cancel":true}
//	↔ {"id":2,"ack":n}     flow control, see below
//
// Flow control: each side may have at most Window unacknowledged stream
// messages in flight per stream (events from the bridge, inputs from the
// daemon). The receiver sends {"id":N,"ack":k} once its consumer has taken k
// messages. Both Stream.Send (bridge) and ClientStream.Send (daemon) block
// while the window is full, so a slow consumer never stalls other calls.
package rpc

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// Window is the per-stream flow-control window, in messages.
const Window = 64

// MaxLine is the largest protocol line accepted (params, results, chunks).
const MaxLine = 16 << 20

// Hello is the first line written by a bridge.
type Hello struct {
	Version string            `json:"version"`
	UID     int               `json:"uid"`
	Admin   bool              `json:"admin"`
	Methods map[string]string `json:"methods"`
	Streams []string          `json:"streams,omitempty"`
}

// Message is one protocol line, in either direction.
type Message struct {
	ID     uint64          `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Stream bool            `json:"stream,omitempty"`
	Input  json.RawMessage `json:"input,omitempty"`
	Cancel bool            `json:"cancel,omitempty"`
	Ack    int             `json:"ack,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *Error          `json:"error,omitempty"`
	Event  string          `json:"event,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
	B64    bool            `json:"b64,omitempty"`
	Hello  *Hello          `json:"hello,omitempty"`
}

// Event names.
const (
	EventData = "data"
	EventEnd  = "end"
)

var errLineTooLong = errors.New("rpc: line too long")

// lineReader reads newline-terminated lines with a size bound. Overlong
// lines are discarded and reported with errLineTooLong; reading can continue.
type lineReader struct {
	br  *bufio.Reader
	max int
}

func newLineReader(r io.Reader, max int) *lineReader {
	return &lineReader{br: bufio.NewReaderSize(r, 64<<10), max: max}
}

func (l *lineReader) next() ([]byte, error) {
	var line []byte
	tooLong := false
	for {
		chunk, err := l.br.ReadSlice('\n')
		if !tooLong {
			if len(line)+len(chunk) > l.max {
				tooLong = true
				line = nil
			} else {
				line = append(line, chunk...)
			}
		}
		switch {
		case err == bufio.ErrBufferFull:
			continue
		case err != nil:
			if err == io.EOF && len(line) > 0 && !tooLong {
				return line, nil
			}
			return nil, err
		}
		if tooLong {
			return nil, errLineTooLong
		}
		return line, nil
	}
}

// lineWriter serialises JSON lines from concurrent goroutines.
type lineWriter struct {
	mu  sync.Mutex
	w   io.Writer
	err error
}

func (l *lineWriter) write(m *Message) error {
	b, err := json.Marshal(m)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.err != nil {
		return l.err
	}
	if _, err := l.w.Write(b); err != nil {
		l.err = err
		return err
	}
	return nil
}
