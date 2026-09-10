package desktop

import (
	"fmt"
	"io"
	"os"
)

// SaveTemp writes a uniquely named file matching pattern in dir (the system
// temporary directory when empty). It returns the path only after writing and
// closing succeed, and removes incomplete output on failure. The caller owns
// the naming, the formatting, and any buffering it needs.
func SaveTemp(dir, pattern string, write func(io.Writer) error) (string, error) {
	file, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return "", fmt.Errorf("create export: %w", err)
	}
	name := file.Name()
	saved := false
	defer func() {
		if !saved {
			_ = file.Close()
			_ = os.Remove(name)
		}
	}()
	if err := write(file); err != nil {
		return "", fmt.Errorf("write export: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close export: %w", err)
	}
	saved = true
	return name, nil
}
