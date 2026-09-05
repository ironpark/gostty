//go:build windows

package raw

// Zig's Windows page allocator calls into ntdll directly. Zig's own linker
// pulls that in implicitly; mingw's ld does not, so the archives above leave
// NtAllocateVirtualMemory and friends undefined without this. The file name
// sorts after the generated link inputs so the flag lands after the archives
// on the link line, which is where ld needs it.

/*
#cgo LDFLAGS: -lntdll
*/
import "C"
