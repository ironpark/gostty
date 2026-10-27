//go:build !gostty_noembed

package gostty

import _ "embed"

//go:embed libs/windows_arm64/gostty_zigo.dll
var embeddedLibrary []byte
