//go:build windows

package terminal

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ptyConn is the master side of a ConPTY: reads return what the console
// renders (VT sequences), writes feed its input.
type ptyConn = *conPTY

type conPTY struct {
	in  *os.File // our write end of the console's input pipe
	out *os.File // our read end of the console's output pipe

	mu     sync.Mutex
	hpc    windows.Handle
	closed bool // pseudo console already closed
}

func (c *conPTY) Read(p []byte) (int, error)  { return c.out.Read(p) }
func (c *conPTY) Write(p []byte) (int, error) { return c.in.Write(p) }

// hangup closes the pseudo console (once). That ends the console host, which
// makes the output pipe return EOF after the pending output is read.
func (c *conPTY) hangup() {
	c.mu.Lock()
	hpc := c.hpc
	if c.closed {
		c.mu.Unlock()
		return
	}
	c.closed = true
	c.mu.Unlock()
	windows.ClosePseudoConsole(hpc)
}

// Close releases everything. Safe to call more than once and while a Read is
// blocked (it then fails with os.ErrClosed).
func (c *conPTY) Close() error {
	_ = c.in.Close()
	_ = c.out.Close() // before hangup: ClosePseudoConsole may wait for output to drain
	c.hangup()
	return nil
}

func (c *conPTY) resize(cols, rows uint16) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return os.ErrClosed
	}
	return windows.ResizePseudoConsole(c.hpc, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// conPTYs maps a pid to its console so hangupProc can find it.
var conPTYs sync.Map // int -> *conPTY

// defaultShell is the shell used when the session names none that exists.
func defaultShell() (path string, args []string) {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if p, err := exec.LookPath(name); err == nil {
			return p, []string{"-NoLogo"}
		}
	}
	if p := os.Getenv("ComSpec"); p != "" {
		return p, nil
	}
	return "cmd.exe", nil
}

// envBlock builds a CreateProcess unicode environment block.
func envBlock(env []string) (*uint16, error) {
	if env == nil {
		env = os.Environ()
	}
	var b []uint16
	for _, kv := range env {
		if strings.IndexByte(kv, 0) >= 0 {
			return nil, errors.New("environment contains a NUL byte")
		}
		b = append(b, utf16.Encode([]rune(kv))...)
		b = append(b, 0)
	}
	if len(b) == 0 {
		b = append(b, 0)
	}
	b = append(b, 0)
	return &b[0], nil
}

// startPTY starts cmd attached to a new ConPTY of cols x rows, as the current
// user. cmd.Path (when it exists; else the default PowerShell), cmd.Args[1:],
// cmd.Dir and cmd.Env are honored. On success cmd.Process is set, so cmd.Wait
// works and yields the exit code. When the process ends the console is closed
// so reads return EOF.
func startPTY(cmd *exec.Cmd, cols, rows uint16) (ptyConn, error) {
	path := cmd.Path
	var args []string
	if len(cmd.Args) > 1 {
		args = cmd.Args[1:]
	}
	if path != "" {
		if p, err := exec.LookPath(path); err == nil {
			path = p
		} else {
			path = ""
		}
	}
	if path == "" {
		path, args = defaultShell()
	}
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(append([]string{path}, args...)))
	if err != nil {
		return nil, err
	}
	appName, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	var dir *uint16
	if cmd.Dir != "" {
		if dir, err = windows.UTF16PtrFromString(cmd.Dir); err != nil {
			return nil, err
		}
	}
	env, err := envBlock(cmd.Env)
	if err != nil {
		return nil, err
	}

	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, fmt.Errorf("create input pipe: %w", err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		windows.CloseHandle(inRead)
		windows.CloseHandle(inWrite)
		return nil, fmt.Errorf("create output pipe: %w", err)
	}
	var hpc windows.Handle
	err = windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, 0, &hpc)
	// The console holds its own references; ours must go so EOF can happen.
	windows.CloseHandle(inRead)
	windows.CloseHandle(outWrite)
	if err != nil {
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		return nil, fmt.Errorf("create pseudo console: %w", err)
	}
	fail := func(err error) (ptyConn, error) {
		windows.CloseHandle(inWrite)
		windows.CloseHandle(outRead)
		windows.ClosePseudoConsole(hpc)
		return nil, err
	}

	al, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return fail(err)
	}
	defer al.Delete()
	if err := al.Update(windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE, *(*unsafe.Pointer)(unsafe.Pointer(&hpc)), unsafe.Sizeof(hpc)); err != nil {
		return fail(err)
	}
	var si windows.StartupInfoEx
	si.Cb = uint32(unsafe.Sizeof(si))
	si.ProcThreadAttributeList = al.List()

	var pi windows.ProcessInformation
	err = windows.CreateProcess(appName, cmdline, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		env, dir, &si.StartupInfo, &pi)
	if err != nil {
		return fail(fmt.Errorf("start process: %w", err))
	}
	windows.CloseHandle(pi.Thread)
	// os.FindProcess opens its own handle; pi.Process keeps the pid from being
	// reused until then.
	proc, ferr := os.FindProcess(int(pi.ProcessId))
	if ferr != nil {
		_ = windows.TerminateProcess(pi.Process, 1)
		windows.CloseHandle(pi.Process)
		return fail(ferr)
	}
	cmd.Process = proc

	c := &conPTY{
		in:  os.NewFile(uintptr(inWrite), "conpty-in"),
		out: os.NewFile(uintptr(outRead), "conpty-out"),
		hpc: hpc,
	}
	pid := int(pi.ProcessId)
	conPTYs.Store(pid, c)
	// When the process ends, close the console so the reader sees EOF (a
	// ConPTY output pipe otherwise stays open), then drop our handle.
	go func(h windows.Handle) {
		_, _ = windows.WaitForSingleObject(h, windows.INFINITE)
		windows.CloseHandle(h)
		conPTYs.Delete(pid)
		c.hangup()
	}(pi.Process)
	return c, nil
}

// resizePTY changes the size of the pty.
func resizePTY(p ptyConn, cols, rows uint16) error {
	return p.resize(cols, rows)
}

// hangupProc closes the process's pseudo console, which tells it its terminal
// went away. There are no process groups to signal, so self is ignored.
func hangupProc(pid int, self bool) {
	if v, ok := conPTYs.Load(pid); ok {
		v.(*conPTY).hangup()
	}
}

// killProc terminates the process.
func killProc(pid int) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.TerminateProcess(h, 1)
}

// isPTYClosed reports whether a read error just means the pty ended (the
// console host closing the pipe shows up as io.EOF or a broken pipe).
func isPTYClosed(err error) bool {
	return errors.Is(err, windows.ERROR_BROKEN_PIPE) || errors.Is(err, os.ErrClosed)
}
