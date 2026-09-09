//go:build !windows

package shell

import "os"

// Var is the environment variable naming the shell on this platform.
const Var = "SHELL"

// The user's shell, or the one every POSIX system has.
func defaultShell() []string {
	if shell := os.Getenv(Var); shell != "" {
		return []string{shell}
	}
	return []string{"/bin/sh"}
}
