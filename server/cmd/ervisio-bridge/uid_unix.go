//go:build !windows

package main

import "os"

// selfUID is the uid the bridge reports in its hello.
func selfUID() int { return os.Geteuid() }
