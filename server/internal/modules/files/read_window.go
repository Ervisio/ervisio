package files

import "github.com/ervisio/ervisio/server/internal/rpc"

// ReadWindow bounds a streaming read without allocating the selected range.
// A nil length reads to EOF; an explicit zero length transfers no bytes.
func ReadWindow(size, offset int64, length *int64) (int64, error) {
	if size < 0 || offset < 0 || offset > size {
		return 0, rpc.Errorf(rpc.Invalid, "The requested file offset is outside the file.")
	}
	available := size - offset
	if length == nil {
		return available, nil
	}
	if *length < 0 || *length > available {
		return 0, rpc.Errorf(rpc.Invalid, "The requested file range is outside the file.")
	}
	return *length, nil
}
