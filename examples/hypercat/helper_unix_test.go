//go:build !windows

package main

import (
	"os/signal"
	"syscall"
)

func ignoreHangup() { signal.Ignore(syscall.SIGHUP) }
