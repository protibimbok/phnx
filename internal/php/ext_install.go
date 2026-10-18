package php

import (
	"fmt"
	"os/exec"
	"regexp"
	"runtime"
	"strings"

	"github.com/protibimbok/phnx/internal/system"
)

var extNameRe = regexp.MustCompile(`^[a-z0-9_]+$`)

// ValidateExtension checks that an extension name is safe to splice into a
// package name (e.g. "redis", "pdo_pgsql").
func ValidateExtension(name string) error {
	if !extNameRe.MatchString(name) {
		return fmt.Errorf("invalid extension name %q — expected lowercase letters, digits or underscores", name)
	}
	return nil
}

// InstallExtension installs the package that provides the given extension for
// the given PHP installation, using the distro's PHP package source.
func InstallExtension(r ResolvedPHP, ext string) error {
	if runtime.GOOS == "darwin" {
		return installExtMac(r.Version, ext)
	}
	switch {
	case system.IsArch():
		return installExtArch(r, ext)
	case system.IsDebian():
		return system.Run("apt-get", "install", "-y", fmt.Sprintf("php%s-%s", r.Version, ext))
	case system.IsFedora():
		return installExtFedora(r.Version, ext)
	default:
		return fmt.Errorf("unsupported distro for automated extension install — please install the %s extension for PHP %s manually", ext, r.Version)
	}
}

func installExtArch(r ResolvedPHP, ext string) error {
	pkg := "php-" + ext
	if r.Tagged {
		pkg = fmt.Sprintf("php%s-%s", system.ArchVersionTag(r.Version), ext)
	}

	if pacmanHasPackage(pkg) {
		fmt.Printf("Installing %s from repos (pre-built)...\n", pkg)
		return system.Run("pacman", "-S", "--noconfirm", "--needed", pkg)
	}

	aurHelper := detectAURHelper()
	if aurHelper != "" {
		fmt.Printf("%s not found in pacman repos — installing via %s...\n", pkg, aurHelper)
		return system.RunUserTTY(aurHelper, "-S", pkg)
	}
	return fmt.Errorf("cannot install %s: package not in pacman repos and no AUR helper found", pkg)
}

func installExtFedora(version, ext string) error {
	tag := system.ArchVersionTag(version)
	// Remi names bundled extensions php84-php-<ext> and PECL ones
	// php84-php-pecl-<ext>; try the bundled name first.
	candidates := []string{
		fmt.Sprintf("php%s-php-%s", tag, ext),
		fmt.Sprintf("php%s-php-pecl-%s", tag, ext),
	}
	for _, pkg := range candidates {
		if exec.Command("dnf", "info", "-q", pkg).Run() == nil {
			return system.Run("dnf", "install", "-y", pkg)
		}
	}
	return fmt.Errorf("no Remi package found for extension %s (tried %s)", ext, strings.Join(candidates, ", "))
}

func installExtMac(version, ext string) error {
	out, _ := exec.Command("brew", "tap").Output()
	if !strings.Contains(string(out), "shivammathur/extensions") {
		fmt.Println("Tapping shivammathur/extensions...")
		// brew must NOT run under sudo
		if err := system.RunUser("brew", "tap", "shivammathur/extensions"); err != nil {
			return err
		}
	}
	return system.RunUser("brew", "install", fmt.Sprintf("shivammathur/extensions/%s@%s", ext, version))
}
