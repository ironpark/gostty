package desktop

import (
	"net/url"
	"os/exec"
	"runtime"
)

// The schemes a link is allowed to open with.
//
// A URI in a link came out of a pty, which is to say out of whatever the shell
// was running, and handing an arbitrary scheme to the platform opener is
// handing it whatever handler is registered for it. These four are what a
// terminal is for.
var openableSchemes = map[string]bool{
	"http": true, "https": true, "mailto": true, "file": true,
}

// openable reports whether a URI is one this program will hand to the platform.
func openable(raw string) bool {
	parsed, err := url.Parse(raw)
	return err == nil && openableSchemes[parsed.Scheme]
}

// OpenURL hands a link to the platform. Failures are the platform's to report:
// there is nothing this program can do about a machine with no browser, and a
// terminal that died because a link did not open would be worse.
func OpenURL(raw string) {
	if !openable(raw) {
		return
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", raw)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", raw)
	default:
		cmd = exec.Command("xdg-open", raw)
	}
	_ = cmd.Start()
	if cmd.Process != nil {
		// Reaped rather than left as a zombie; the opener exits as soon as it
		// has handed the link on.
		go func() { _ = cmd.Wait() }()
	}
}
