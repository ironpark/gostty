//go:build !windows

package main

import (
	"fmt"
	"os"
)

func describeStdio() string {
	return fmt.Sprintf("stdout=%d stdin=%d", os.Stdout.Fd(), os.Stdin.Fd())
}

const conoutMarker = ""
