package php

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// corePackageSuffixes are package-name suffixes that are parts of PHP itself
// rather than extensions (e.g. php8.3-fpm, php84-php-common); removing them
// would take PHP down with the "extension".
var corePackageSuffixes = map[string]bool{
	"cli": true, "fpm": true, "common": true, "cgi": true, "dev": true, "devel": true, "phpdbg": true,
}

// IsCorePackage reports whether an extension name actually names a core PHP
// package that must not be removed as an extension.
func IsCorePackage(ext string) bool {
	return corePackageSuffixes[ext]
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

// UninstallExtension removes the package that provides the given extension for
// the given PHP installation. It reports false (and does nothing) when no
// separately installed package provides it, i.e. the extension ships with PHP.
func UninstallExtension(r ResolvedPHP, ext string) (bool, error) {
	if IsCorePackage(ext) {
		return false, fmt.Errorf("%s is part of PHP itself, not an extension", ext)
	}
	if runtime.GOOS == "darwin" {
		formula := fmt.Sprintf("%s@%s", ext, r.Version)
		if _, err := os.Stat(filepath.Join(system.Platform.BrewPrefix, "Cellar", formula)); err != nil {
			return false, nil
		}
		// brew must NOT run under sudo
		return true, system.RunUser("brew", "uninstall", "shivammathur/extensions/"+formula)
	}
	switch {
	case system.IsArch():
		pkg := "php-" + ext
		if r.Tagged {
			pkg = fmt.Sprintf("php%s-%s", system.ArchVersionTag(r.Version), ext)
		}
		if exec.Command("pacman", "-Q", pkg).Run() != nil {
			return false, nil
		}
		return true, system.Run("pacman", "-Rns", "--noconfirm", pkg)
	case system.IsDebian():
		pkg := fmt.Sprintf("php%s-%s", r.Version, ext)
		// Virtual packages (e.g. php8.3-pdo, provided by php8.3-common) have no
		// install status of their own, so they read as "not installed" here.
		out, _ := exec.Command("dpkg-query", "-W", "-f=${Status}", pkg).Output()
		if !strings.Contains(string(out), "install ok installed") {
			return false, nil
		}
		return true, system.Run("apt-get", "remove", "-y", pkg)
	case system.IsFedora():
		tag := system.ArchVersionTag(r.Version)
		for _, pkg := range []string{
			fmt.Sprintf("php%s-php-%s", tag, ext),
			fmt.Sprintf("php%s-php-pecl-%s", tag, ext),
		} {
			if exec.Command("rpm", "-q", pkg).Run() == nil {
				return true, system.Run("dnf", "remove", "-y", pkg)
			}
		}
		return false, nil
	default:
		return false, fmt.Errorf("unsupported distro for automated extension removal — please remove the %s extension for PHP %s manually", ext, r.Version)
	}
}
