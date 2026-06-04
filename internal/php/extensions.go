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

// commentedExtensionRe matches a commented-out extension directive in a php.ini,
// e.g. ";extension=bcmath" or "  ;zend_extension=opcache". It deliberately does
// NOT match "extension_dir" (the char after "extension" must be "=" / spaces),
// and only matches lines whose sole content is the directive.
var commentedExtensionRe = regexp.MustCompile(`^(\s*);\s*((?:zend_)?extension)\s*=\s*(\S+)\s*$`)

// EnableExtensions uncomments every ";extension=" / ";zend_extension=" line in the
// php.ini file(s) for the given PHP installation, so the bundled extensions are
// actually loaded. When the extension's module (.so) is missing from the
// extension_dir the line is left commented to avoid noisy FPM startup warnings.
//
// It returns the names of the extensions it newly enabled. It is idempotent: lines
// that are already active are left untouched, so re-running enables nothing new.
// The caller is responsible for (re)starting the FPM service to pick up changes.
func EnableExtensions(r ResolvedPHP) ([]string, error) {
	files := iniFiles(r)
	if len(files) == 0 {
		return nil, nil
	}

	extDir := extensionDir(r.Binary)

	var enabled []string
	seen := map[string]bool{}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			continue // ini not present on this layout (e.g. cli vs fpm split)
		}
		names, err := enableInFile(f, extDir)
		if err != nil {
			return enabled, err
		}
		for _, n := range names {
			if !seen[n] {
				seen[n] = true
				enabled = append(enabled, n)
			}
		}
	}
	return enabled, nil
}

// enableInFile uncomments extension directives in a single php.ini and writes it
// back (escalating to sudo when needed). Returns the names it activated.
func enableInFile(path, extDir string) ([]string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	lines := strings.Split(string(content), "\n")
	var enabled []string
	for i, line := range lines {
		m := commentedExtensionRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		indent, directive, value := m[1], m[2], strings.Trim(m[3], `"`)
		if extDir != "" && !moduleExists(extDir, value) {
			continue // .so not installed — leave it commented
		}
		lines[i] = fmt.Sprintf("%s%s=%s", indent, directive, value)
		enabled = append(enabled, strings.TrimSuffix(value, ".so"))
	}

	if len(enabled) == 0 {
		return nil, nil
	}
	if err := system.WriteFile(path, strings.Join(lines, "\n")); err != nil {
		return nil, err
	}
	return enabled, nil
}

// moduleExists reports whether the shared object for an extension value (e.g.
// "bcmath" or "opcache.so") is present in extDir.
func moduleExists(extDir, value string) bool {
	name := value
	if !strings.HasSuffix(name, ".so") {
		name += ".so"
	}
	_, err := os.Stat(filepath.Join(extDir, name))
	return err == nil
}

// extensionDir returns the configured extension_dir for the given php binary,
// or "" if it cannot be determined (in which case callers enable every directive).
func extensionDir(binary string) string {
	if binary == "" {
		binary = "php"
	}
	out, err := exec.Command(binary, "-d", "display_errors=0", "-r",
		`echo ini_get("extension_dir");`).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// iniFiles returns the php.ini path(s) that govern the given PHP installation.
// Some layouts (Debian) split fpm and cli configuration into separate files.
func iniFiles(r ResolvedPHP) []string {
	v := r.Version
	if runtime.GOOS == "darwin" {
		return []string{filepath.Join(system.Platform.BrewPrefix, "etc", "php", v, "php.ini")}
	}
	switch {
	case system.IsArch():
		if r.Tagged {
			return []string{fmt.Sprintf("/etc/php%s/php.ini", system.ArchVersionTag(v))}
		}
		return []string{"/etc/php/php.ini"}
	case system.IsDebian():
		return []string{
			fmt.Sprintf("/etc/php/%s/fpm/php.ini", v),
			fmt.Sprintf("/etc/php/%s/cli/php.ini", v),
		}
	case system.IsFedora():
		return []string{fmt.Sprintf("/etc/opt/remi/php%s/php.ini", system.ArchVersionTag(v))}
	default:
		return nil
	}
}
