//go:build windows

package plugins

import (
	"context"
	"errors"
	"net"
	"os"
	"time"

	"golang.org/x/sys/windows"
)

// dialPipe opens a Windows named pipe (\\.\pipe\name), waiting while every
// instance is busy, and returns it as a net.Conn for the HTTP client.
func dialPipe(ctx context.Context, name string) (net.Conn, error) {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}
	for {
		h, err := windows.CreateFile(p, windows.GENERIC_READ|windows.GENERIC_WRITE, 0, nil,
			windows.OPEN_EXISTING, windows.FILE_FLAG_OVERLAPPED|windows.SECURITY_SQOS_PRESENT|windows.SECURITY_IDENTIFICATION, 0)
		if err == nil {
			f := os.NewFile(uintptr(h), name)
			return &pipeConn{f: f, name: name}, nil
		}
		if !errors.Is(err, windows.ERROR_PIPE_BUSY) {
			return nil, &os.PathError{Op: "open", Path: name, Err: err}
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

type pipeAddr string

func (a pipeAddr) Network() string { return "pipe" }
func (a pipeAddr) String() string  { return string(a) }

// pipeConn is an overlapped pipe handle; os.File gives it deadlines.
type pipeConn struct {
	f    *os.File
	name string
}

func (c *pipeConn) Read(b []byte) (int, error)         { return c.f.Read(b) }
func (c *pipeConn) Write(b []byte) (int, error)        { return c.f.Write(b) }
func (c *pipeConn) Close() error                       { return c.f.Close() }
func (c *pipeConn) LocalAddr() net.Addr                { return pipeAddr(c.name) }
func (c *pipeConn) RemoteAddr() net.Addr               { return pipeAddr(c.name) }
func (c *pipeConn) SetDeadline(t time.Time) error      { return c.f.SetDeadline(t) }
func (c *pipeConn) SetReadDeadline(t time.Time) error  { return c.f.SetReadDeadline(t) }
func (c *pipeConn) SetWriteDeadline(t time.Time) error { return c.f.SetWriteDeadline(t) }
