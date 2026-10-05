//go:build windows

package logs

import (
	"os"
	"syscall"
)

// openShared opens a file for reading with every share flag, FILE_SHARE_DELETE
// included: Go's os.Open does not share deletes, so a followed log could not
// be renamed or removed by its application's rotation.
func openShared(path string) (*os.File, error) {
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	h, err := syscall.CreateFile(p, syscall.GENERIC_READ,
		syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE,
		nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	return os.NewFile(uintptr(h), path), nil
}

// fileInode returns an identity of the file at path: the volume serial and
// the file index of GetFileInformationByHandle (Windows has no inode in
// os.FileInfo), folded into 64 bits. A different identity means the path now
// names another file: the log was rotated.
func fileInode(path string, _ os.FileInfo) (uint64, bool) {
	f, err := openShared(path)
	if err != nil {
		return 0, false
	}
	defer f.Close()
	var bi syscall.ByHandleFileInformation
	if err := syscall.GetFileInformationByHandle(syscall.Handle(f.Fd()), &bi); err != nil {
		return 0, false
	}
	idx := uint64(bi.FileIndexHigh)<<32 | uint64(bi.FileIndexLow)
	return idx ^ uint64(bi.VolumeSerialNumber)*0x9E3779B97F4A7C15, true
}
