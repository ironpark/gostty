package shell

import (
	"fmt"
	"path"
	"strings"
)

// QuotePaths prepares filenames to paste as arguments in the shell started by
// this session, joined by spaces. It cannot detect a different shell
// subsequently started inside it.
func (s *Session) QuotePaths(paths []string) (string, error) {
	return quotePaths(s.command, paths)
}

func quotePaths(command string, paths []string) (string, error) {
	name := strings.TrimSuffix(strings.ToLower(path.Base(strings.ReplaceAll(command, `\`, "/"))), ".exe")
	quoted := make([]string, 0, len(paths))
	for _, filename := range paths {
		if strings.ContainsAny(filename, "\x00\r\n") {
			return "", fmt.Errorf("cannot paste a path containing a line break or NUL")
		}
		switch name {
		case "cmd":
			// cmd expands percent variables even inside quotes, and may have delayed
			// !variable! expansion enabled. Reject these rather than changing a path.
			// https://learn.microsoft.com/windows-server/administration/windows-commands/cmd
			if strings.ContainsAny(filename, "\"%!") {
				return "", fmt.Errorf("cannot quote a path containing quotes, %% or ! for cmd.exe")
			}
			quoted = append(quoted, `"`+filename+`"`)
		case "powershell", "pwsh":
			// PowerShell literal strings escape apostrophes by doubling them.
			// https://learn.microsoft.com/powershell/module/microsoft.powershell.core/about/about_quoting_rules
			quoted = append(quoted, "'"+strings.ReplaceAll(filename, "'", "''")+"'")
		default:
			quoted = append(quoted, "'"+strings.ReplaceAll(filename, "'", `'\''`)+"'")
		}
	}
	if len(quoted) == 0 {
		return "", nil
	}
	return strings.Join(quoted, " "), nil
}
