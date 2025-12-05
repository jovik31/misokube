package cniplugin

import (
	"log"
	"os"
	"path/filepath"
)

// LoadLogFile configures the global logger to write to a file.
// Controlled by env var LOG_FILE; default is "/var/log/setera-cni.log".
// Returns the opened *os.File or nil if logging to file is disabled or failed.
func LoadLogFile() *os.File {
	logFile := os.Getenv("LOG_FILE")
	if logFile == "" {
		logFile = "/var/log/setera-cni.log"
	}

	f, err := openLogFile(logFile)
	if err != nil {
		// Fall back to stderr; keep running.
		return nil
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags | log.Lshortfile | log.Lmicroseconds)
	return f
}

func openLogFile(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	// Append mode; create if not exists; readable by root.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return f, nil
}
