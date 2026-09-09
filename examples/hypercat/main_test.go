package main

import (
	"testing"

	"github.com/ironpark/gostty/examples/hypercat/internal/shelltest"
)

// The tab tests run this binary as their shell, so it has to be able to be one.
func TestMain(m *testing.M) { shelltest.Main(m) }
