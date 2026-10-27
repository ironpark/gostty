//go:build !gostty_noembed

package gostty

import _ "embed"

//go:embed libs/linux_arm64/libgostty_zigo.so
var embeddedLibrary []byte
