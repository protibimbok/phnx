package database

import (
	"fmt"
	"os/exec"
	"strings"
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

func Create(host string, port int, user, password, name string) error {
	client, err := ClientBinary()
	if err != nil {
		return err
	}
	args := []string{
		"-h", host,
		"-P", fmt.Sprintf("%d", port),
		"-u", user,
	}
	if password != "" {
		args = append(args, "-p"+password)
	}
	args = append(args, "-e", fmt.Sprintf("CREATE DATABASE IF NOT EXISTS `%s`;", name))
	if out, err := exec.Command(client, args...).CombinedOutput(); err != nil {
		return fmt.Errorf("%w\n%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func SanitizeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), "_")
	if out == "" {
		return "app"
	}
	return out
}
