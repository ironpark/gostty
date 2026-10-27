//go:build !gostty_noembed

package gostty

import _ "embed"

//go:embed libs/linux_amd64/libgostty_zigo.so
var embeddedLibrary []byte
