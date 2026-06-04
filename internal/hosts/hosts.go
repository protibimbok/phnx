package hosts

import (
	"fmt"
	"os"
	"strings"

	"github.com/protibimbok/phnx/internal/system"
)

const (
	hostsFile  = "/etc/hosts"
	blockStart = "# phnx-start"
	blockEnd   = "# phnx-end"
)

// Add inserts "127.0.0.1 domain" into the phnx block (or creates it).
func Add(domain string) error {
	if domain == "" || domain == "localhost" {
		return nil
	}
	current, err := read()
	if err != nil {
		return err
	}
	entry := fmt.Sprintf("127.0.0.1 %s", domain)
	updated := addEntry(current, entry)
	return write(updated)
}

// Remove deletes the line for the given domain from the phnx block.
func Remove(domain string) error {
	if domain == "" {
		return nil
	}
	current, err := read()
	if err != nil {
		return err
	}
	entry := fmt.Sprintf("127.0.0.1 %s", domain)
	updated := removeEntry(current, entry)
	return write(updated)
}

// HasEntry reports whether the domain is already in the hosts file.
func HasEntry(domain string) (bool, error) {
	current, err := read()
	if err != nil {
		return false, err
	}
	entry := fmt.Sprintf("127.0.0.1 %s", domain)
	for _, line := range strings.Split(current, "\n") {
		if strings.TrimSpace(line) == entry {
			return true, nil
		}
	}
	return false, nil
}

func read() (string, error) {
	data, err := os.ReadFile(hostsFile)
	if err != nil {
		return "", fmt.Errorf("reading %s: %w", hostsFile, err)
	}
	return string(data), nil
}

func write(content string) error {
	return system.WriteFile(hostsFile, content)
}

func addEntry(content, entry string) string {
	startIdx := strings.Index(content, blockStart)
	endIdx := strings.Index(content, blockEnd)

	if startIdx == -1 || endIdx == -1 {
		// No block yet — append one
		if !strings.HasSuffix(content, "\n") {
			content += "\n"
		}
		return content + blockStart + "\n" + entry + "\n" + blockEnd + "\n"
	}

	// Block exists — check if entry is already there
	block := content[startIdx : endIdx+len(blockEnd)]
	if strings.Contains(block, entry) {
		return content // already present
	}

	// Insert before blockEnd
	before := content[:endIdx]
	after := content[endIdx:]
	return before + entry + "\n" + after
}

func removeEntry(content, entry string) string {
	startIdx := strings.Index(content, blockStart)
	endIdx := strings.Index(content, blockEnd)

	if startIdx == -1 || endIdx == -1 {
		return content
	}

	before := content[:startIdx]
	blockContent := content[startIdx : endIdx+len(blockEnd)+1]
	after := content[endIdx+len(blockEnd):]
	if strings.HasSuffix(after, "\n") && len(after) > 1 {
		// trim leading newline from after if block was its own section
	}

	// Remove entry from block
	lines := strings.Split(blockContent, "\n")
	var kept []string
	for _, l := range lines {
		if strings.TrimSpace(l) != entry {
			kept = append(kept, l)
		}
	}
	newBlock := strings.Join(kept, "\n")

	// If block is now empty (only markers), remove the whole block
	innerLines := []string{}
	for _, l := range kept {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" && trimmed != blockStart && trimmed != blockEnd {
			innerLines = append(innerLines, l)
		}
	}
	if len(innerLines) == 0 {
		return strings.TrimRight(before, "\n") + "\n" + strings.TrimLeft(after, "\n")
	}

	return before + newBlock + after
}
