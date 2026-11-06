//go:build windows

package server

import "testing"

func TestPersonalStateRejectsOtherWindowsWriters(t *testing.T) {
	path := privateStateTemp(t)
	if err := privateStateDirectory(path); err != nil {
		t.Fatalf("private temporary folder refused: %v", err)
	}
	makeWorldWritable(t, path)
	if err := privateStateDirectory(path); err == nil {
		t.Fatal("accepted state writable by Everyone")
	}
}
