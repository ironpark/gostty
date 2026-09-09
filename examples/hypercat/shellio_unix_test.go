//go:build !windows

package main

import "testing"

func skipWithoutShellIO(t *testing.T) {}
