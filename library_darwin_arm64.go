//go:build !gostty_noembed

package gostty

import _ "embed"

//go:embed libs/darwin_arm64/libgostty_zigo.dylib
var embeddedLibrary []byte
