package main

import "os"

// `SHELL` is a POSIX convention; Windows names its command processor in
// `COMSPEC`.
const shellVar = "COMSPEC"

// The command processor Windows names, or the one it has always shipped.
func defaultShell() []string {
	if shell := os.Getenv(shellVar); shell != "" {
		return []string{shell}
	}
	return []string{`C:\Windows\System32\cmd.exe`}
}
