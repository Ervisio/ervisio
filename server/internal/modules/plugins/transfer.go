package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Large transfers (plugins.download / plugins.upload in the SDK). The
// daemon's /api/plugins/transfer endpoints open these bridge streams and
// pipe a browser download or upload through them; nothing is buffered
// beyond the stream's flow-control window, and the 16 MB protocol line and
// the 8 MB HTTP result limit do not apply.
const (
	// defaultMaxUpload is capabilities.http[].maxUpload when it is not set.
	defaultMaxUpload = int64(20) << 30
	// execDownloadStderr is how much of a command's stderr is kept for its
	// error message.
	execDownloadStderr = 4 << 10
)

// UploadParams are the params of plugins.httpUpload.
type UploadParams struct {
	HTTPParams
	// Size is the exact size in bytes of the file that follows as input.
	Size int64 `json:"size"`
	// Stream sends the response as it arrives (a build that answers with
	// progress lines while the context uploads): {"status","headers"}, then
	// binary chunks, then {"done":true,"status","headers"}. Without it the
	// response is one final event, capped like plugins.http.
	Stream bool `json:"stream,omitempty"`
}

// runHTTPDownload is plugins.httpDownload: plugins.httpStream for GET only.
// It sends {"status","headers"} when the response starts, then the body as
// binary chunks, and ends when the response has been read to its end.
func runHTTPDownload(ctx context.Context, c *rpc.Call, s rpc.Stream, p HTTPParams) error {
	if p.Method != http.MethodGet {
		return rpc.Errorf(rpc.Invalid, "A download uses GET, not %q.", p.Method)
	}
	return runHTTPStream(ctx, c, s, p)
}

// runExecDownload is plugins.execDownload: the standard output of a declared
// command, as binary chunks. {"status":200,"headers":{}} is sent with the
// first output (or at the end of an empty one), so a command that fails
// before it writes anything is reported as an error, not as a broken file.
// A non-zero exit ends the stream with an error.
func runExecDownload(ctx context.Context, c *rpc.Call, s rpc.Stream, p ExecParams) error {
	r, cmd, err := resolveCmd(ctx, c, p)
	if err != nil {
		return err
	}
	so, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errb limitBuf
	errb.max = execDownloadStderr
	cmd.Stderr = &errb
	if err := cmd.Start(); err != nil {
		return rpc.Errorf(rpc.Unavailable, "Could not run %s: %v", r.argv[0], err)
	}
	start := map[string]any{"status": 200, "headers": map[string]string{}}
	started, ok := pipeChunks(cmd, s, so, start)
	if !ok {
		return nil
	}
	werr := cmd.Wait()
	if ctx.Err() != nil {
		return nil
	}
	if werr != nil {
		return execDownloadError(p.Command, r.argv[0], werr, errb.buf.String())
	}
	if !started {
		return s.Send(start)
	}
	return nil
}

// pipeChunks sends the command's output as binary chunks, with start before
// the first one. ok is false when a send failed: the command is then killed
// and reaped.
func pipeChunks(cmd *exec.Cmd, s rpc.Stream, so io.Reader, start map[string]any) (started, ok bool) {
	buf := make([]byte, httpChunk)
	for {
		n, rerr := so.Read(buf)
		if n > 0 {
			if !started {
				started = true
				if err := s.Send(start); err != nil {
					abortCmd(cmd)
					return started, false
				}
			}
			if err := s.SendBytes(buf[:n]); err != nil {
				abortCmd(cmd)
				return started, false
			}
		}
		if rerr != nil {
			return started, true
		}
	}
}

func abortCmd(cmd *exec.Cmd) {
	_ = cmd.Process.Kill()
	_ = cmd.Wait()
}

// execDownloadError turns a failed Wait into the stream's error.
func execDownloadError(command, prog string, werr error, stderr string) error {
	var ee *exec.ExitError
	if !errors.As(werr, &ee) {
		return rpc.Errorf(rpc.Unavailable, "Could not run %s: %v", prog, werr)
	}
	if !utf8.ValidString(stderr) {
		stderr = ""
	}
	return rpc.Errorf(rpc.Unavailable, "%s failed with exit code %d: %s", command, ee.ExitCode(), stderr)
}

// runHTTPUpload is plugins.httpUpload. The rules are checked first, then
// {"ready":true} is sent; the client then sends {"data":"<base64>"} inputs
// and {"eof":true}. The bytes become the request body (POST or PUT only,
// Content-Length = Size). The last event is {"done":true,"status","headers",
// "body","b64","truncated"} like plugins.http, or with Stream the response
// body follows as chunks (see UploadParams.Stream).
// uploadIdle is how long an upload may go without data before the bridge
// gives up on it and closes the request to the service.
var uploadIdle = 2 * time.Minute

func runHTTPUpload(ctx context.Context, c *rpc.Call, s rpc.Stream, p UploadParams) error {
	pl, err := resolveUpload(ctx, c, p)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := s.Send(map[string]bool{"ready": true}); err != nil {
		return nil
	}

	pr, pw := io.Pipe()
	defer pr.Close()
	// The request to the service ends with the stream: when the daemon
	// cancels it (the browser went away, the session ended) or no data
	// came for uploadIdle, the pipe fails, so the HTTP client aborts the
	// request and closes the connection. A service left waiting for the rest
	// of a body may hold a lock meanwhile (Docker's container lock).
	idle := time.NewTimer(uploadIdle)
	defer idle.Stop()
	stalled := rpc.Errorf(rpc.Unavailable, "The upload stalled: no data came for %s.", uploadIdle)
	var isStalled atomic.Bool
	go func() {
		var got int64
		for {
			select {
			case <-ctx.Done():
				pw.CloseWithError(ctx.Err())
				return
			case <-idle.C:
				isStalled.Store(true)
				pw.CloseWithError(stalled)
				cancel() // also when the client is stuck writing to the service
				return
			case in, open := <-s.Input():
				idle.Reset(uploadIdle)
				if !open {
					pw.CloseWithError(context.Canceled)
					return
				}
				var finished bool
				if got, finished = writeUploadInput(pw, in, got, p.Size); finished {
					return
				}
			}
		}
	}()

	resp, err := doUpload(ctx, pl, pr, p.Size)
	if err != nil && isStalled.Load() {
		return stalled
	}
	if err != nil {
		// A bad upload (short, too long) fails the body read: say so.
		var re *rpc.Error
		if errors.As(err, &re) {
			return re
		}
		return httpErr(ctx, err, pl)
	}
	defer resp.Body.Close()
	if p.Stream {
		return streamUploadResponse(ctx, s, resp, pl)
	}
	return sendUploadResponse(ctx, s, resp, pl)
}

// resolveUpload checks the upload's method, body and size on top of the
// rules of resolveHTTP.
func resolveUpload(ctx context.Context, c *rpc.Call, p UploadParams) (*httpPlan, error) {
	if p.Method != http.MethodPost && p.Method != http.MethodPut {
		return nil, rpc.Errorf(rpc.Invalid, "An upload uses POST or PUT, not %q.", p.Method)
	}
	if p.Body != "" {
		return nil, rpc.Errorf(rpc.Invalid, "An upload takes its body from the file, not from body.")
	}
	pl, err := resolveHTTP(ctx, c, p.HTTPParams)
	if err != nil {
		return nil, err
	}
	limit := pl.api.MaxUpload
	if limit == 0 {
		limit = defaultMaxUpload
	}
	if p.Size < 0 {
		return nil, rpc.Errorf(rpc.Invalid, "Give the size of the file.")
	}
	if p.Size > limit {
		return nil, rpc.Errorf(rpc.Invalid, "The file is larger than the %d bytes %s accepts for uploads.", limit, pl.api.Name)
	}
	return pl, nil
}

// writeUploadInput handles one {"data"} or {"eof"} input. It returns the
// bytes received so far and whether the pipe is finished (closed, or its
// reader gone).
func writeUploadInput(pw *io.PipeWriter, in json.RawMessage, got, size int64) (int64, bool) {
	var msg struct {
		Data string `json:"data"`
		EOF  bool   `json:"eof"`
	}
	if err := json.Unmarshal(in, &msg); err != nil {
		pw.CloseWithError(rpc.Errorf(rpc.Invalid, "Malformed upload data."))
		return got, true
	}
	if msg.EOF {
		if got != size {
			pw.CloseWithError(rpc.Errorf(rpc.Invalid, "The upload ended early (%d of %d bytes). Try again.", got, size))
			return got, true
		}
		pw.Close()
		return got, true
	}
	b, err := base64.StdEncoding.DecodeString(msg.Data)
	if err != nil {
		pw.CloseWithError(rpc.Errorf(rpc.Invalid, "Malformed upload data."))
		return got, true
	}
	got += int64(len(b))
	if got > size {
		pw.CloseWithError(rpc.Errorf(rpc.Invalid, "The upload is larger than the %d bytes announced.", size))
		return got, true
	}
	if _, err := pw.Write(b); err != nil {
		return got, true
	}
	return got, false
}

// doUpload sends the planned request with body as its size bytes.
func doUpload(ctx context.Context, pl *httpPlan, body io.Reader, size int64) (*http.Response, error) {
	req := pl.req.WithContext(ctx)
	req.ContentLength = size
	if size == 0 {
		req.Body = http.NoBody
	} else {
		req.Body = io.NopCloser(body)
	}
	return unixClient(pl.socket, pl.timeout).Do(req)
}

// streamUploadResponse sends {"status","headers"}, the body as binary
// chunks, then {"done":true,"status","headers"}.
func streamUploadResponse(ctx context.Context, s rpc.Stream, resp *http.Response, pl *httpPlan) error {
	hdr := flatHeaders(resp.Header)
	if err := s.Send(map[string]any{"status": resp.StatusCode, "headers": hdr}); err != nil {
		return nil
	}
	if sent, err := streamBody(ctx, s, resp.Body, pl); !sent {
		return err
	}
	return s.Send(map[string]any{"done": true, "status": resp.StatusCode, "headers": hdr})
}

// sendUploadResponse sends the response as one {"done":true,...} event,
// its body cut to the result limit.
func sendUploadResponse(ctx context.Context, s rpc.Stream, resp *http.Response, pl *httpPlan) error {
	limit := min(pl.maxBody, maxHTTPResult)
	b, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return httpErr(ctx, err, pl)
	}
	res := map[string]any{"done": true, "status": resp.StatusCode, "headers": flatHeaders(resp.Header)}
	if int64(len(b)) > limit {
		b = b[:limit]
		res["truncated"] = true
	}
	body, isB64 := encodeBody(b)
	res["body"] = body
	if isB64 {
		res["b64"] = true
	}
	return s.Send(res)
}
