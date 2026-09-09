package shell

import "os"

// Var is the environment variable naming the shell on this platform. `SHELL`
// is a POSIX convention; Windows names its command processor in `COMSPEC`.
const Var = "COMSPEC"

// The command processor Windows names, or the one it has always shipped.
func defaultShell() []string {
	if shell := os.Getenv(Var); shell != "" {
		return []string{shell}
	}
	return []string{`C:\Windows\System32\cmd.exe`}
}
