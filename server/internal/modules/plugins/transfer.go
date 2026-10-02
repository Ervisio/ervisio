package plugins

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os/exec"
	"unicode/utf8"

	"github.com/ervisio/ervisio/server/internal/rpc"
	"github.com/ervisio/ervisio/server/internal/sys"
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
	r, err := resolve(c, p, false)
	if err != nil {
		return err
	}
	cmd, err := sys.Cmd{Name: r.argv[0], Args: r.argv[1:], Timeout: -1}.Command(ctx)
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
	started := false
	buf := make([]byte, httpChunk)
	for {
		n, rerr := so.Read(buf)
		if n > 0 {
			if !started {
				started = true
				if err := s.Send(start); err != nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					return nil
				}
			}
			if err := s.SendBytes(buf[:n]); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return nil
			}
		}
		if rerr != nil {
			break
		}
	}
	werr := cmd.Wait()
	if ctx.Err() != nil {
		return nil
	}
	if werr != nil {
		var ee *exec.ExitError
		if errors.As(werr, &ee) {
			msg := errb.buf.String()
			if !utf8.ValidString(msg) {
				msg = ""
			}
			return rpc.Errorf(rpc.Unavailable, "%s failed with exit code %d: %s", p.Command, ee.ExitCode(), msg)
		}
		return rpc.Errorf(rpc.Unavailable, "Could not run %s: %v", r.argv[0], werr)
	}
	if !started {
		return s.Send(start)
	}
	return nil
}

// runHTTPUpload is plugins.httpUpload. The rules are checked first, then
// {"ready":true} is sent; the client then sends {"data":"<base64>"} inputs
// and {"eof":true}. The bytes become the request body (POST or PUT only,
// Content-Length = Size). The last event is {"done":true,"status","headers",
// "body","b64","truncated"} like plugins.http, or with Stream the response
// body follows as chunks (see UploadParams.Stream).
func runHTTPUpload(ctx context.Context, c *rpc.Call, s rpc.Stream, p UploadParams) error {
	if p.Method != http.MethodPost && p.Method != http.MethodPut {
		return rpc.Errorf(rpc.Invalid, "An upload uses POST or PUT, not %q.", p.Method)
	}
	if p.Body != "" {
		return rpc.Errorf(rpc.Invalid, "An upload takes its body from the file, not from body.")
	}
	pl, err := resolveHTTP(ctx, c, p.HTTPParams)
	if err != nil {
		return err
	}
	limit := pl.api.MaxUpload
	if limit == 0 {
		limit = defaultMaxUpload
	}
	if p.Size < 0 {
		return rpc.Errorf(rpc.Invalid, "Give the size of the file.")
	}
	if p.Size > limit {
		return rpc.Errorf(rpc.Invalid, "The file is larger than the %d bytes %s accepts for uploads.", limit, pl.api.Name)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if err := s.Send(map[string]bool{"ready": true}); err != nil {
		return nil
	}

	pr, pw := io.Pipe()
	defer pr.Close()
	go func() {
		var got int64
		for {
			select {
			case <-ctx.Done():
				pw.CloseWithError(ctx.Err())
				return
			case in, open := <-s.Input():
				if !open {
					pw.CloseWithError(context.Canceled)
					return
				}
				var msg struct {
					Data string `json:"data"`
					EOF  bool   `json:"eof"`
				}
				if err := json.Unmarshal(in, &msg); err != nil {
					pw.CloseWithError(rpc.Errorf(rpc.Invalid, "Malformed upload data."))
					return
				}
				if msg.EOF {
					if got != p.Size {
						pw.CloseWithError(rpc.Errorf(rpc.Invalid, "The upload ended early (%d of %d bytes). Try again.", got, p.Size))
						return
					}
					pw.Close()
					return
				}
				b, err := base64.StdEncoding.DecodeString(msg.Data)
				if err != nil {
					pw.CloseWithError(rpc.Errorf(rpc.Invalid, "Malformed upload data."))
					return
				}
				got += int64(len(b))
				if got > p.Size {
					pw.CloseWithError(rpc.Errorf(rpc.Invalid, "The upload is larger than the %d bytes announced.", p.Size))
					return
				}
				if _, err := pw.Write(b); err != nil {
					return
				}
			}
		}
	}()

	req := pl.req.WithContext(ctx)
	req.ContentLength = p.Size
	if p.Size == 0 {
		req.Body = http.NoBody
	} else {
		req.Body = io.NopCloser(pr)
	}
	resp, err := unixClient(pl.api.Socket, pl.timeout).Do(req)
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
		hdr := flatHeaders(resp.Header)
		if err := s.Send(map[string]any{"status": resp.StatusCode, "headers": hdr}); err != nil {
			return nil
		}
		buf := make([]byte, httpChunk)
		for {
			n, rerr := resp.Body.Read(buf)
			if n > 0 {
				if err := s.SendBytes(buf[:n]); err != nil {
					return nil
				}
			}
			if errors.Is(rerr, io.EOF) {
				break
			}
			if rerr != nil {
				if ctx.Err() != nil {
					return nil
				}
				return rpc.Errorf(rpc.Unavailable, "The response from %s broke off: %v", pl.api.Socket, rerr)
			}
		}
		return s.Send(map[string]any{"done": true, "status": resp.StatusCode, "headers": hdr})
	}
	limitRes := min(pl.maxBody, maxHTTPResult)
	b, err := io.ReadAll(io.LimitReader(resp.Body, limitRes+1))
	if err != nil {
		return httpErr(ctx, err, pl)
	}
	truncated := int64(len(b)) > limitRes
	if truncated {
		b = b[:limitRes]
	}
	res := map[string]any{"done": true, "status": resp.StatusCode, "headers": flatHeaders(resp.Header)}
	if truncated {
		res["truncated"] = true
	}
	if utf8.Valid(b) && jsonTextLen(b) <= jsonTextBudget {
		res["body"] = string(b)
	} else {
		res["body"], res["b64"] = base64.StdEncoding.EncodeToString(b), true
	}
	return s.Send(res)
}
