package system

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type ServiceManager interface {
	Start(name string) error
	Stop(name string) error
	Restart(name string) error
	Enable(name string) error
	Disable(name string) error
	IsRunning(name string) (bool, error)
}

func NewServiceManager() ServiceManager {
	if runtime.GOOS == "darwin" {
		return &brewServices{}
	}
	return &systemctlService{}
}

// systemctlService — Linux systemd
type systemctlService struct{}

func (s *systemctlService) Start(name string) error {
	return Run("systemctl", "start", name)
}
func (s *systemctlService) Stop(name string) error {
	return Run("systemctl", "stop", name)
}
func (s *systemctlService) Restart(name string) error {
	return Run("systemctl", "restart", name)
}
func (s *systemctlService) Enable(name string) error {
	return Run("systemctl", "enable", name)
}
func (s *systemctlService) Disable(name string) error {
	return Run("systemctl", "disable", name)
}
func (s *systemctlService) IsRunning(name string) (bool, error) {
	cmd := exec.Command("systemctl", "is-active", "--quiet", name)
	err := cmd.Run()
	if err != nil {
		if _, ok := err.(*exec.ExitError); ok {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// brewServices — macOS Homebrew services
type brewServices struct{}

func (b *brewServices) Start(name string) error   { return runBrew("start", name) }
func (b *brewServices) Stop(name string) error    { return runBrew("stop", name) }
func (b *brewServices) Restart(name string) error { return runBrew("restart", name) }
func (b *brewServices) Enable(name string) error  { return runBrew("start", name) }
func (b *brewServices) Disable(name string) error { return runBrew("stop", name) }

func (b *brewServices) IsRunning(name string) (bool, error) {
	cmd := exec.Command("brew", "services", "list")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return false, fmt.Errorf("brew services list: %w", err)
	}
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, name) && strings.Contains(line, "started") {
			return true, nil
		}
	}
	return false, nil
}

func runBrew(action, name string) error {
	// brew must NOT run under sudo
	return RunUser("brew", "services", action, name)
}

// PHPFPMServiceName returns the systemd/brew service name for a PHP-FPM version.
func PHPFPMServiceName(version string) string {
	if runtime.GOOS == "darwin" {
		return fmt.Sprintf("php@%s", version)
	}
	if IsArch() {
		tag := ArchVersionTag(version)
		return fmt.Sprintf("php%s-fpm", tag)
	}
	if IsFedora() {
		// Remi software collections: php82-php-fpm.service
		return fmt.Sprintf("php%s-php-fpm", ArchVersionTag(version))
	}
	// Debian/Ubuntu ondrej PPA
	return fmt.Sprintf("php%s-fpm", version)
}
