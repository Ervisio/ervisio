package server

import (
	"context"
	"fmt"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

// Native image/audio/video decoding belongs to the browser. Active documents
// never become inline content through this endpoint, including renamed HTML.
var mediaMIME = map[string]bool{
	"image/png": true, "image/jpeg": true, "image/gif": true, "image/webp": true, "image/avif": true,
	"image/bmp": true, "image/x-icon": true, "image/vnd.microsoft.icon": true, "image/svg+xml": true,
	"image/tiff": true, "image/heic": true, "image/heif": true,
	"video/mp4": true, "video/webm": true, "video/ogg": true, "video/quicktime": true, "video/x-m4v": true,
	"video/x-matroska": true, "video/x-msvideo": true,
	"audio/mpeg": true, "audio/mp4": true, "audio/ogg": true, "audio/wav": true, "audio/x-wav": true,
	"audio/flac": true, "audio/x-flac": true, "audio/webm": true, "audio/aac": true, "audio/opus": true, "audio/vnd.wave": true, "audio/x-m4a": true,
}

type mediaRange struct {
	start, length int64
	partial       bool
}

// Only one byte range is accepted: media elements seek using single ranges.
// Unsupported range units are ignored, as required by RFC 9110 section 14.
func parseMediaRange(value string, size int64) (mediaRange, error) {
	whole := mediaRange{length: size}
	if value == "" {
		return whole, nil
	}
	if len(value) > 256 {
		return whole, fmt.Errorf("invalid byte range")
	}
	unit, spec, ok := strings.Cut(value, "=")
	if !ok || strings.TrimSpace(unit) != "bytes" {
		return whole, nil
	}
	if strings.Contains(spec, ",") {
		return whole, fmt.Errorf("multiple byte ranges are not supported")
	}
	first, last, ok := strings.Cut(strings.TrimSpace(spec), "-")
	if !ok || size <= 0 {
		return whole, fmt.Errorf("unsatisfiable byte range")
	}
	if first == "" {
		suffix, err := strconv.ParseInt(last, 10, 64)
		if err != nil || suffix <= 0 {
			return whole, fmt.Errorf("unsatisfiable byte range")
		}
		length := min(suffix, size)
		return mediaRange{size - length, length, true}, nil
	}
	start, err := strconv.ParseInt(first, 10, 64)
	if err != nil || start < 0 || start >= size {
		return whole, fmt.Errorf("unsatisfiable byte range")
	}
	end := size - 1
	if last != "" {
		end, err = strconv.ParseInt(last, 10, 64)
		if err != nil || end < start {
			return whole, fmt.Errorf("unsatisfiable byte range")
		}
		end = min(end, size-1)
	}
	return mediaRange{start, end - start + 1, true}, nil
}

func mediaRangeHeader(r *http.Request, mtime int64) string {
	if r.Method == http.MethodHead {
		return ""
	}
	value := r.Header.Get("Range")
	if validator := r.Header.Get("If-Range"); validator != "" {
		date, err := http.ParseTime(validator)
		if err != nil || mtime <= 0 || !time.UnixMilli(mtime).Truncate(time.Second).Equal(date) {
			return ""
		}
	}
	return value
}

func (s *Server) handleMedia(w http.ResponseWriter, r *http.Request, sess *Session) {
	target, e := transferParams(r)
	if e != nil {
		writeError(w, e)
		return
	}
	method := target.method("files.readStream", "remote.mediaStream")
	bridge, isAdmin, e := s.route(r.Context(), sess, method, target.admin)
	if e != nil {
		writeError(w, e)
		return
	}
	defer sess.hold(bridge, isAdmin)()
	probe := target.params()
	probe["metadataOnly"] = true
	probeContext, stopProbe := context.WithTimeout(r.Context(), 20*time.Second)
	defer stopProbe()
	info, err := bridge.Stream(probeContext, method, probe)
	if err != nil {
		writeError(w, rpc.ToError(err, false))
		return
	}
	meta, e := readDownloadMeta(info, method)
	if e == nil {
		complete := false
		for event := range info.Events() {
			if event.B64 {
				e = rpc.Errorf(rpc.Internal, "The media metadata probe returned unexpected file data.")
				break
			}
			complete = complete || isDoneEvent(event.Data)
		}
		if e == nil && (!complete || info.Err() != nil) {
			e = rpc.Errorf(rpc.Unavailable, "The media metadata probe did not finish.")
		}
	}
	info.Close()
	stopProbe()
	if e != nil {
		writeError(w, e)
		return
	}
	mediaType, _, parseErr := mime.ParseMediaType(meta.Mime)
	if parseErr != nil || !mediaMIME[mediaType] || meta.Size < 0 {
		writeError(w, rpc.Errorf(rpc.Invalid, "This file cannot be displayed as a photo, video or audio recording."))
		return
	}
	span, err := parseMediaRange(mediaRangeHeader(r, meta.MTime), meta.Size)
	if err != nil {
		w.Header().Set("Content-Range", fmt.Sprintf("bytes */%d", meta.Size))
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusRequestedRangeNotSatisfiable)
		return
	}
	var stream *rpc.ClientStream
	if r.Method != http.MethodHead {
		params := target.params()
		params["offset"] = span.start
		params["length"] = span.length
		stream, err = bridge.Stream(r.Context(), method, params)
		if err != nil {
			writeError(w, rpc.ToError(err, false))
			return
		}
		defer stream.Close()
		fresh, problem := readDownloadMeta(stream, method)
		if problem != nil {
			writeError(w, problem)
			return
		}
		if fresh.Size != span.length || fresh.TotalSize != meta.Size || fresh.Mime != meta.Mime || fresh.MTime != meta.MTime {
			writeError(w, rpc.Errorf(rpc.Conflict, "The media file changed. Open it again."))
			return
		}
	}
	h := w.Header()
	h.Set("Content-Type", meta.Mime)
	h.Set("Content-Disposition", "inline; filename*=UTF-8''"+url.PathEscape(meta.Name))
	h.Set("Content-Security-Policy", "sandbox; default-src 'none'")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Cache-Control", "private, no-store")
	h.Set("Accept-Ranges", "bytes")
	h.Set("Content-Length", strconv.FormatInt(span.length, 10))
	if meta.MTime > 0 {
		h.Set("Last-Modified", time.UnixMilli(meta.MTime).UTC().Format(http.TimeFormat))
	}
	status := http.StatusOK
	if span.partial {
		status = http.StatusPartialContent
		h.Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", span.start, span.start+span.length-1, meta.Size))
	}
	w.WriteHeader(status)
	if r.Method == http.MethodHead {
		return
	}
	var written int64
	done := false
	for event := range stream.Events() {
		if !event.B64 {
			done = done || isDoneEvent(event.Data)
			continue
		}
		chunk, ok := decodeChunk(event)
		if !ok || int64(len(chunk)) > span.length-written {
			panic(http.ErrAbortHandler)
		}
		n, writeErr := w.Write(chunk)
		written += int64(n)
		if writeErr != nil {
			return
		}
	}
	if !done || stream.Err() != nil || written != span.length {
		if r.Context().Err() == nil {
			s.log.Printf("media transfer for %q ended early (%d bytes): %v", sess.Account.Name, written, stream.Err())
		}
		panic(http.ErrAbortHandler)
	}
}
