package database

import (
	"fmt"
	"os/exec"
)

// ClientBinary returns the preferred mysql-compatible CLI binary name.
// Prefers mariadb over the deprecated mysql compatibility symlink.
func ClientBinary() (string, error) {
	if _, err := exec.LookPath("mariadb"); err == nil {
		return "mariadb", nil
	}
	if _, err := exec.LookPath("mysql"); err == nil {
		return "mysql", nil
	}
	return "", fmt.Errorf("mysql or mariadb client not found in PATH")
}
