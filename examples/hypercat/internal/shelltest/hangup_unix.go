//go:build !windows

package shelltest

import (
	"os/signal"
	"syscall"
)

func ignoreHangup() { signal.Ignore(syscall.SIGHUP) }
