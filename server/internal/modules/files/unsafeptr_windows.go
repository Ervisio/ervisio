//go:build windows

package files

import "unsafe"

// attrTagInfo is FILE_ATTRIBUTE_TAG_INFO (not defined by x/sys/windows).
type attrTagInfo struct {
	FileAttributes uint32
	ReparseTag     uint32
}

const fileAttributeTagInfoClass = 9 // FILE_INFO_BY_HANDLE_CLASS: FileAttributeTagInfo

var sizeofTagInfo = unsafe.Sizeof(attrTagInfo{})

func unsafePtr(i *attrTagInfo) unsafe.Pointer { return unsafe.Pointer(i) }
