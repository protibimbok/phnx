package nginx

import (
	"runtime"

	"github.com/protibimbok/phnx/internal/system"
)

// Test runs nginx -t to validate the configuration.
func Test() error {
	if runtime.GOOS == "darwin" {
		// Homebrew nginx runs as current user — no sudo needed
		_, err := system.OutputUser("nginx", "-t")
		return err
	}
	_, err := system.Output("nginx", "-t")
	return err
}

// Reload tests the config then signals nginx to reload without dropping connections.
func Reload() error {
	if err := Test(); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		// "brew services reload" doesn't exist; send HUP via nginx -s reload.
		// Homebrew nginx is user-owned, so no sudo.
		return system.RunUser("nginx", "-s", "reload")
	}
	return system.Run("systemctl", "reload", "nginx")
}

// Restart tests the config then does a full nginx restart.
func Restart() error {
	if err := Test(); err != nil {
		return err
	}
	if runtime.GOOS == "darwin" {
		return system.RunUser("brew", "services", "restart", "nginx")
	}
	return system.Run("systemctl", "restart", "nginx")
}
