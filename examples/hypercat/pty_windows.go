package main

// startOnPty opens a pseudoconsole, sizes it, and starts `argv` on it.
func startOnPty(cols, rows int, argv, env []string) (terminalDevice, shellProcess, error) {
	console, err := newConPty(cols, rows)
	if err != nil {
		return nil, nil, err
	}
	process, err := console.start(argv, env)
	if err != nil {
		_ = console.Close()
		return nil, nil, err
	}
	return console, process, nil
}

// A pseudoconsole outlives the program running in it, so a read on the output
// pipe does not end when the shell does. Closing the console -- and only the
// console -- is what ends it, after the last screen has been flushed.
func endReadsAfterExit(device terminalDevice) {
	if console, ok := device.(*conPty); ok {
		console.endReads()
	}
}
