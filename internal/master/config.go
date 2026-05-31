package master

import (
	"os"
	"path/filepath"
	"strings"
)

type Config struct {
	BaseHost string
	DataDir  string
}

func loadConfig() Config {
	baseHost := strings.ToLower(strings.TrimSpace(os.Getenv("POCKETHOST_BASE_HOST")))
	if baseHost == "" {
		baseHost = "pocketbase.example.com"
	}
	dataDir := strings.TrimSpace(os.Getenv("POCKETHOST_DATA_DIR"))
	if dataDir == "" {
		dataDir = filepath.Join(".", "pb_data")
	}
	if abs, err := filepath.Abs(dataDir); err == nil {
		dataDir = abs
	}
	return Config{BaseHost: baseHost, DataDir: dataDir}
}
