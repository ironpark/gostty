package desktop

import (
	"fmt"
	"io"
	"os"
)

// SaveHTML writes a uniquely named HTML file in dir (the system temporary
// directory when empty). It returns the path only after writing and closing
// succeed, and removes incomplete output on failure. The caller owns formatting.
func SaveHTML(dir string, write func(io.Writer) error) (string, error) {
	file, err := os.CreateTemp(dir, "hypercat-*.html")
	if err != nil {
		return "", fmt.Errorf("create HTML export: %w", err)
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
		return "", fmt.Errorf("write HTML export: %w", err)
	}
	if err := file.Close(); err != nil {
		return "", fmt.Errorf("close HTML export: %w", err)
	}
	saved = true
	return name, nil
}
