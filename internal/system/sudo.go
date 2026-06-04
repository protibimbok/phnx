package system

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Run executes a command with sudo (when not already root).
func Run(args ...string) error {
	cmd := sudoCmd(args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("sudo %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// Output executes a command with sudo and returns combined output.
func Output(args ...string) (string, error) {
	cmd := sudoCmd(args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("sudo %s: %w\n%s", strings.Join(args, " "), err, buf.String())
	}
	return buf.String(), nil
}

// RunUser runs a command as the current user WITHOUT sudo.
// Use this for tools like brew that must not run under sudo.
func RunUser(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// RunUserTTY runs an interactive command as the current user, connecting
// stdin/stdout/stderr directly to /dev/tty. This ensures the subprocess
// always gets a real TTY — and thus real-time, unbuffered output — even
// when a framework like bubbletea has redirected os.Stdout through a pipe.
// Falls back to RunUser if /dev/tty cannot be opened (e.g. in CI).
func RunUserTTY(args ...string) error {
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return RunUser(args...)
	}
	defer tty.Close()
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = tty
	cmd.Stderr = tty
	cmd.Stdin = tty
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// RunUserMenuInput runs a command as the current user, piping a stream of
// newlines as stdin. This auto-accepts the default option on any interactive
// menu prompts (e.g. yay's "cleanBuild?" / "Diffs to show?" menus) while
// stdout/stderr are still streamed to the terminal.
func RunUserMenuInput(args ...string) error {
	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = &infiniteNewlines{}
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return nil
}

// infiniteNewlines is an io.Reader that returns newline bytes indefinitely,
// used to accept default answers on interactive menu prompts.
type infiniteNewlines struct{}

func (infiniteNewlines) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = '\n'
	}
	return len(p), nil
}

// OutputUser runs a command as the current user and returns its output.
func OutputUser(args ...string) (string, error) {
	cmd := exec.Command(args[0], args[1:]...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return buf.String(), fmt.Errorf("%s: %w\n%s", strings.Join(args, " "), err, buf.String())
	}
	return buf.String(), nil
}

// WriteFile writes content to path. Tries a direct write first; if permission
// is denied it falls back to sudo tee (so macOS Homebrew paths work without sudo,
// and system paths like /etc/hosts still work with sudo).
func WriteFile(path, content string) error {
	err := os.WriteFile(path, []byte(content), 0644)
	if err == nil {
		return nil
	}
	if !errors.Is(err, os.ErrPermission) {
		return fmt.Errorf("writing %s: %w", path, err)
	}
	// Permission denied — escalate via sudo tee
	cmd := exec.Command("sudo", "tee", path)
	cmd.Stdin = strings.NewReader(content)
	cmd.Stdout = os.Stdout
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err2 := cmd.Run(); err2 != nil {
		return fmt.Errorf("writing %s: %w\n%s", path, err2, errBuf.String())
	}
	return nil
}

// MkdirAll creates directories, escalating to sudo only if needed.
func MkdirAll(path string) error {
	if err := os.MkdirAll(path, 0755); err == nil {
		return nil
	}
	return Run("mkdir", "-p", path)
}

// RemoveFile removes a file, escalating to sudo only if needed.
func RemoveFile(path string) error {
	if err := os.Remove(path); err == nil {
		return nil
	}
	return Run("rm", "-f", path)
}

func sudoCmd(args ...string) *exec.Cmd {
	if os.Getuid() == 0 {
		return exec.Command(args[0], args[1:]...)
	}
	return exec.Command("sudo", args...)
}
