//go:build windows

package files

// The safe file system primitives rely on directory descriptors (openat and
// friends), which Windows does not have. Until a Windows implementation exists
// every operation that changes or reads through them is refused. A weaker
// variant that follows links is deliberately not offered.

import (
	"context"

	"github.com/ervisio/ervisio/server/internal/rpc"
)

func errUnsupported() error {
	return rpc.Errorf(rpc.Unavailable, "This operation is not supported on Windows yet.")
}

func checkExists(path string) error             { return errUnsupported() }
func mkdirOne(path string, perm uint32) error   { return errUnsupported() }
func createFile(path string, perm uint32) error { return errUnsupported() }
func mkdirAll(path string, perm uint32) error   { return errUnsupported() }
func removePath(path string) error              { return errUnsupported() }
func movePath(from, to string) error            { return errUnsupported() }

func doCopy(ctx context.Context, p copyParams, s rpc.Stream) error { return errUnsupported() }

func hChmod(ctx context.Context, c *rpc.Call) (any, error) { return nil, errUnsupported() }
func hChown(ctx context.Context, c *rpc.Call) (any, error) { return nil, errUnsupported() }

func writeTextFile(path string, data []byte, expected int64) (any, error) {
	return nil, errUnsupported()
}

func hWriteStream(ctx context.Context, c *rpc.Call, s rpc.Stream) error { return errUnsupported() }
