package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// DiscoverProjectPrompts walks up from workDir looking for SOUL.md and AGENTS.md.
// Returns soul prompt and agent prompt content.
func DiscoverProjectPrompts(workDir string) (soul, agents string) {
	soul = findAndRead(workDir, "SOUL.md")
	agents = findAndRead(workDir, "AGENTS.md")
	return
}

// findAndRead walks up from dir looking for filename, returns its content.
func findAndRead(dir, filename string) string {
	dir = filepath.Clean(dir)
	for {
		path := filepath.Join(dir, filename)
		data, err := os.ReadFile(path)
		if err == nil {
			content := strings.TrimSpace(string(data))
			if content != "" {
				return content
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
