package main

import (
	"fmt"
	"os"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var procUpdateProcThreadAttribute = windows.NewLazySystemDLL("kernel32.dll").NewProc("UpdateProcThreadAttribute")

// A pseudoconsole and the two pipe ends this process keeps.
//
// This is written out rather than taken from a library because both of the Go
// ones set `STARTF_USESTDHANDLES` in the startup info while leaving the three
// handles it points at empty. CreateProcess reads that as "the child's standard
// handles are these", and these are nothing, so the child comes up with invalid
// stdin and stdout: it writes into the void and reads end of file at once. The
// pseudoconsole attribute is what hands the console over, and it only gets to
// do that if nothing else has claimed the handles first.
type conPty struct {
	console windows.Handle
	// What this process reads the screen from and writes keystrokes to.
	output *os.File
	input  *os.File

	consoleOnce sync.Once
	closeOnce   sync.Once
	closeErr    error
}

func newConPty(cols, rows int) (*conPty, error) {
	// Two pipes, crossed: the console reads what this process writes and writes
	// what it reads. The console takes one end of each.
	consoleInput, ourInput, err := conPtyPipe()
	if err != nil {
		return nil, err
	}
	ourOutput, consoleOutput, err := conPtyPipe()
	if err != nil {
		consoleInput.Close()
		ourInput.Close()
		return nil, err
	}

	var console windows.Handle
	err = windows.CreatePseudoConsole(
		windows.Coord{X: int16(cols), Y: int16(rows)},
		windows.Handle(consoleInput.Fd()),
		windows.Handle(consoleOutput.Fd()),
		0,
		&console,
	)
	// The console has its own duplicates now. Keeping the write end of the
	// output pipe here would mean a read on the other end never reaching the
	// end of the file, however many programs came and went.
	consoleInput.Close()
	consoleOutput.Close()
	if err != nil {
		ourInput.Close()
		ourOutput.Close()
		return nil, fmt.Errorf("create pseudoconsole: %w", err)
	}
	return &conPty{console: console, output: ourOutput, input: ourInput}, nil
}

func conPtyPipe() (read, write *os.File, err error) {
	var r, w windows.Handle
	security := windows.SecurityAttributes{InheritHandle: 0}
	security.Length = uint32(unsafe.Sizeof(security))
	if err := windows.CreatePipe(&r, &w, &security, 0); err != nil {
		return nil, nil, fmt.Errorf("create pipe: %w", err)
	}
	return os.NewFile(uintptr(r), "conpty"), os.NewFile(uintptr(w), "conpty"), nil
}

func (p *conPty) Read(b []byte) (int, error)  { return p.output.Read(b) }
func (p *conPty) Write(b []byte) (int, error) { return p.input.Write(b) }

func (p *conPty) Resize(cols, rows int) error {
	return windows.ResizePseudoConsole(p.console, windows.Coord{X: int16(cols), Y: int16(rows)})
}

// endReads closes the console alone, which is what lets a read on the output
// pipe end. Closing it flushes what the program last drew and then releases the
// console's copy of the pipe's write end, so the reader drains that screen and
// only then reaches the end of the file. Closing the pipe here instead would
// take the screen with it.
func (p *conPty) endReads() {
	p.consoleOnce.Do(func() { windows.ClosePseudoConsole(p.console) })
}

func (p *conPty) Close() error {
	p.closeOnce.Do(func() {
		p.endReads()
		p.closeErr = firstError(p.input.Close(), p.output.Close())
	})
	return p.closeErr
}

func firstError(errs ...error) error {
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	return nil
}

// start runs `argv` attached to this pseudoconsole.
func (p *conPty) start(argv, env []string) (*conPtyProcess, error) {
	attributes, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, fmt.Errorf("attribute list: %w", err)
	}
	defer attributes.Delete()
	// Called rather than going through `attributes.Update`, which takes the
	// value as an `unsafe.Pointer`. A pseudoconsole attribute's value is the
	// handle itself, not somewhere to read it from, so that spelling means
	// converting a handle to a pointer -- which is what it looks like, and what
	// `go vet` rightly says about it. Passed as the integer it is instead.
	ret, _, updateErr := procUpdateProcThreadAttribute.Call(
		uintptr(unsafe.Pointer(attributes.List())),
		0,
		windows.PROC_THREAD_ATTRIBUTE_PSEUDOCONSOLE,
		uintptr(p.console),
		unsafe.Sizeof(p.console),
		0,
		0,
	)
	if ret == 0 {
		return nil, fmt.Errorf("attach pseudoconsole: %w", updateErr)
	}

	// No `STARTF_USESTDHANDLES`. Both of the Go pseudoconsole libraries set it
	// while leaving the three handles it points at empty, which tells
	// CreateProcess the child's standard handles are nothing at all; the
	// console can only supply them if nothing has claimed them first.
	startupInfo := new(windows.StartupInfoEx)
	startupInfo.ProcThreadAttributeList = attributes.List()
	startupInfo.Cb = uint32(unsafe.Sizeof(*startupInfo))

	command, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(argv))
	if err != nil {
		return nil, fmt.Errorf("command line for %s: %w", argv[0], err)
	}
	block := environmentBlock(env)

	var info windows.ProcessInformation
	if err := windows.CreateProcess(
		nil,
		command,
		nil,
		nil,
		false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT,
		&block[0],
		nil,
		&startupInfo.StartupInfo,
		&info,
	); err != nil {
		return nil, fmt.Errorf("start %s: %w", argv[0], err)
	}
	windows.CloseHandle(info.Thread)
	return &conPtyProcess{handle: info.Process}, nil
}

// The environment as CreateProcess wants it: every "name=value" terminated by a
// zero, and one more zero to end the block.
//
// Assembled here rather than by handing the joined string to
// `UTF16PtrFromString`, which rejects a string with a zero in it -- and the
// zeroes are the whole point of the shape.
func environmentBlock(env []string) []uint16 {
	block := make([]uint16, 0, 64)
	for _, entry := range env {
		// A variable with a zero inside it cannot be spelled in the block, and
		// is not something the process could have been given in the first place.
		encoded, err := windows.UTF16FromString(entry)
		if err != nil {
			continue
		}
		block = append(block, encoded...)
	}
	return append(block, 0)
}

// The process handle, waited on directly rather than through `os.Process`: the
// handle is already here, and looking one up by process id again would be a
// race against the id being reused.
type conPtyProcess struct {
	mu     sync.Mutex
	handle windows.Handle
}

func (p *conPtyProcess) Wait() error {
	if _, err := windows.WaitForSingleObject(p.handle, windows.INFINITE); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle != 0 {
		windows.CloseHandle(p.handle)
		p.handle = 0
	}
	return nil
}

func (p *conPtyProcess) Kill() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.handle == 0 {
		return nil // already reaped
	}
	return windows.TerminateProcess(p.handle, 1)
}
