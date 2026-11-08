// Package paths resolves the COAL data directory.
package paths

import (
	"errors"
	"os"
	"path/filepath"
)

// EnvHome names the environment variable that overrides the data directory.
const EnvHome = "COAL_HOME"

// Subdirs lists the directories that live under the data directory.
var Subdirs = []string{"manifests", "artifacts", "sessions", "queue", "trash", "logs"}

// Home returns the data directory. An explicit override wins, then
// COAL_HOME, then ~/.coal. The result is absolute.
func Home(override string) (string, error) {
	dir := override
	if dir == "" {
		dir = os.Getenv(EnvHome)
	}
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		if home == "" {
			return "", errors.New("cannot find the home directory; set COAL_HOME")
		}
		dir = filepath.Join(home, ".coal")
	}
	return filepath.Abs(dir)
}
