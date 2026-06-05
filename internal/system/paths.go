package system

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

type Paths struct {
	NginxDir      string
	NginxSitesDir string
	NginxLogDir   string
	BrewPrefix    string // macOS only
	PhpFpmPoolDir func(version string) string
	PhpBinaryPath func(version string) string
	PhpFpmSock    func(version string) string
}

var Platform *Paths

func init() {
	switch runtime.GOOS {
	case "darwin":
		prefix := detectBrewPrefix()
		Platform = &Paths{
			NginxDir:      prefix + "/etc/nginx",
			NginxSitesDir: prefix + "/etc/nginx/phnx-sites",
			NginxLogDir:   prefix + "/var/log/nginx",
			BrewPrefix:    prefix,
			PhpFpmPoolDir: func(v string) string {
				return fmt.Sprintf("%s/etc/php/%s/fpm/conf.d", prefix, v)
			},
			PhpBinaryPath: func(v string) string {
				return fmt.Sprintf("%s/bin/php%s", prefix, v)
			},
			PhpFpmSock: func(v string) string {
				return detectMacFPMSock(prefix, v)
			},
		}
	default: // linux — distro-aware at call time
		Platform = &Paths{
			NginxDir:      "/etc/nginx",
			NginxSitesDir: "/etc/nginx/phnx-sites",
			NginxLogDir:   "/var/log/nginx",
			PhpFpmPoolDir: linuxFpmPoolDir,
			PhpBinaryPath: linuxPhpBinary,
			PhpFpmSock:    linuxFpmSock,
		}
	}
}

// detectBrewPrefix returns the Homebrew prefix: /opt/homebrew (Apple Silicon)
// or /usr/local (Intel).
func detectBrewPrefix() string {
	// Try brew --prefix first (most reliable)
	if out, err := exec.Command("brew", "--prefix").Output(); err == nil {
		p := strings.TrimSpace(string(out))
		if p != "" {
			return p
		}
	}
	// Fallback: check known locations
	if _, err := os.Stat("/opt/homebrew/bin/brew"); err == nil {
		return "/opt/homebrew"
	}
	return "/usr/local"
}

// detectMacFPMSock tries php-fpm -i to find the configured socket path.
func detectMacFPMSock(prefix, version string) string {
	// shivammathur/php tap names the binary php-fpm@8.2 or php-fpm8.2
	candidates := []string{
		fmt.Sprintf("%s/bin/php-fpm@%s", prefix, version),
		fmt.Sprintf("%s/bin/php-fpm%s", prefix, version),
		fmt.Sprintf("%s/opt/php@%s/sbin/php-fpm", prefix, version),
	}
	for _, bin := range candidates {
		if _, err := os.Stat(bin); err != nil {
			continue
		}
		out, err := exec.Command(bin, "-i").Output()
		if err != nil {
			continue
		}
		for line := range strings.SplitSeq(string(out), "\n") {
			if strings.Contains(line, "listen =>") {
				parts := strings.SplitN(line, "=>", 2)
				if len(parts) == 2 {
					sock := strings.TrimSpace(parts[1])
					if strings.HasPrefix(sock, "/") {
						return sock
					}
				}
			}
		}
	}
	// Common fallback paths
	sockCandidates := []string{
		fmt.Sprintf("%s/var/run/php@%s.sock", prefix, version),
		fmt.Sprintf("%s/var/run/php%s.sock", prefix, version),
	}
	for _, p := range sockCandidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return fmt.Sprintf("%s/var/run/php@%s.sock", prefix, version)
}

// linuxFpmPoolDir returns the FPM pool directory for the given version.
func linuxFpmPoolDir(version string) string {
	if IsArch() {
		return fmt.Sprintf("/etc/php%s/php-fpm.d", ArchVersionTag(version))
	}
	if IsFedora() {
		// Remi software collections: /etc/opt/remi/php82/php-fpm.d
		return fmt.Sprintf("/etc/opt/remi/php%s/php-fpm.d", ArchVersionTag(version))
	}
	return fmt.Sprintf("/etc/php/%s/fpm/pool.d", version)
}

// linuxPhpBinary returns the PHP binary path for the given version.
func linuxPhpBinary(version string) string {
	if IsArch() {
		return fmt.Sprintf("/usr/bin/php%s", ArchVersionTag(version))
	}
	if IsFedora() {
		// Remi software collections install the CLI under the SCL prefix, not in
		// the default PATH. /usr/bin/php only exists if the separate
		// php<tag>-syspaths package is installed (which makes it the system
		// default and conflicts with parallel versions), so use the canonical
		// per-version path: /opt/remi/php82/root/usr/bin/php.
		return fmt.Sprintf("/opt/remi/php%s/root/usr/bin/php", ArchVersionTag(version))
	}
	return fmt.Sprintf("/usr/bin/php%s", version)
}

// linuxFpmSock returns the FPM socket path for a given PHP version.
func linuxFpmSock(version string) string {
	if IsArch() {
		tag := ArchVersionTag(version)
		candidates := []string{
			fmt.Sprintf("/run/php%s-fpm.sock", tag),
			fmt.Sprintf("/var/run/php%s-fpm.sock", tag),
			fmt.Sprintf("/run/php%s/php-fpm.sock", tag),
			fmt.Sprintf("/run/php-fpm/php%s.sock", tag),
			"/run/php-fpm/php-fpm.sock",
		}
		for _, p := range candidates {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return fmt.Sprintf("/run/php%s-fpm.sock", tag)
	}
	if IsFedora() {
		// Remi software collections ship a per-version run dir created by the
		// php<tag>-php-fpm package; the phnx pool listens there.
		tag := ArchVersionTag(version)
		return fmt.Sprintf("/var/opt/remi/php%s/run/php-fpm/phnx.sock", tag)
	}
	return fmt.Sprintf("/run/php/php%s-fpm.sock", version)
}

func IsLinux() bool { return runtime.GOOS == "linux" }
func IsMacOS() bool { return runtime.GOOS == "darwin" }
