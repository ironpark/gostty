package shell

import (
	"os/exec"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestQuotePathsUsesSessionShell(t *testing.T) {
	for _, tt := range []struct {
		name, command string
		paths         []string
		want          string
	}{
		{"posix", "/bin/sh", []string{"/tmp/plain.txt", "/tmp/two words.txt", "/tmp/it's.txt"}, `'/tmp/plain.txt' '/tmp/two words.txt' '/tmp/it'\''s.txt' `},
		{"cmd", `C:\Windows\System32\cmd.exe`, []string{`C:\two words\it's & (ok).txt`}, `"C:\two words\it's & (ok).txt" `},
		{"powershell", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, []string{`C:\it's $HOME.txt`}, `'C:\it''s $HOME.txt' `},
		{"pwsh", "/usr/bin/pwsh", []string{"/tmp/it's.txt"}, `'/tmp/it''s.txt' `},
		{"empty", "cmd.exe", nil, ""},
	} {
		t.Run(tt.name, func(t *testing.T) {
			s := &Session{command: tt.command}
			got, err := s.QuotePaths(tt.paths)
			if err != nil || got != tt.want {
				t.Fatalf("QuotePaths = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestQuotePathsRejectsUnrepresentableInputWithoutPartialPaste(t *testing.T) {
	for _, command := range []string{"sh", "cmd.exe", "pwsh"} {
		for _, filename := range []string{"a\nb", "a\rb", "a\x00b"} {
			got, err := quotePaths(command, []string{"ordinary", filename})
			if err == nil || got != "" {
				t.Errorf("%s accepted %q: %q, %v", command, filename, got, err)
			}
		}
	}
	for _, filename := range []string{`C:\%TEMP%\a`, `C:\!TEMP!\a`, `C:\a"b`} {
		got, err := quotePaths("cmd.exe", []string{"ordinary", filename})
		if err == nil || got != "" {
			t.Errorf("cmd accepted %q: %q, %v", filename, got, err)
		}
	}
}

// Check the actual shell parser, including expansions and command separators.
func TestPOSIXDropPathsRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a POSIX shell")
	}
	paths := []string{"/tmp/plain", "/tmp/two words", "/tmp/it's", `/tmp/$(echo expanded);$HOME&|`, `/tmp/back\slash`}
	text, err := quotePaths("/bin/sh", paths)
	if err != nil {
		t.Fatal(err)
	}
	output, err := exec.Command("/bin/sh", "-c", "printf '%s\\000' "+text).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSuffix(string(output), "\x00"), "\x00")
	if !reflect.DeepEqual(got, paths) {
		t.Fatalf("shell received %q, want %q", got, paths)
	}
}
