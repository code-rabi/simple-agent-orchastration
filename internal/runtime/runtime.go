package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

func AgentAPIBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	name := "agentapi"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(exe), "libexec", name), nil
}
