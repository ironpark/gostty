//go:build !windows

package main

import "os"

// The environment variable naming the shell on this platform.
const shellVar = "SHELL"

// The user's shell, or the one every POSIX system has.
func defaultShell() []string {
	if shell := os.Getenv(shellVar); shell != "" {
		return []string{shell}
	}
	return []string{"/bin/sh"}
}
