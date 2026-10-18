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
	// On Fedora/RHEL, Remi packages each extension separately and ships an
	// already-active, load-ordered drop-in under /etc/opt/remi/phpXX/php.d/
	// (e.g. 20-mysqlnd.ini before 30-mysqli.ini). PHP parses the main php.ini
	// before scanning php.d, so uncommenting "extension=mysqli" in php.ini loads
	// it ahead of mysqlnd and crashes with "undefined symbol:
	// mysqlnd_global_stats". Extension enablement is the packages' job here.
	if system.IsFedora() {
		return nil, nil
	}

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
		names, err := enableInFile(f, extDir, nil)
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
// back (escalating to sudo when needed). When only is non-nil, just the named
// extensions are touched. Returns the names it activated.
func enableInFile(path, extDir string, only map[string]bool) ([]string, error) {
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
		if only != nil && !only[extName(value)] {
			continue
		}
		if extDir != "" && !moduleExists(extDir, value) {
			continue // .so not installed — leave it commented
		}
		lines[i] = fmt.Sprintf("%s%s=%s", indent, directive, value)
		enabled = append(enabled, extName(value))
	}

	if len(enabled) == 0 {
		return nil, nil
	}
	if err := system.WriteFile(path, strings.Join(lines, "\n")); err != nil {
		return nil, err
	}
	return enabled, nil
}

// extName normalises an extension directive value ("opcache.so",
// "/usr/lib/php/modules/redis.so") to its bare name ("opcache", "redis").
func extName(value string) string {
	return strings.ToLower(strings.TrimSuffix(filepath.Base(value), ".so"))
}

// EnableExtension uncomments the directive for a single extension in the php.ini
// file(s) and the scanned conf.d directory of the given PHP installation. It
// reports whether any file was changed. Like EnableExtensions it is a no-op on
// Fedora, where Remi packages ship active drop-ins.
func EnableExtension(r ResolvedPHP, name string) (bool, error) {
	if system.IsFedora() {
		return false, nil
	}
	name = strings.ToLower(name)
	files := iniFiles(r)
	if dir := scanDir(r.Binary); dir != "" {
		matches, _ := filepath.Glob(filepath.Join(dir, "*.ini"))
		files = append(files, matches...)
	}

	extDir := extensionDir(r.Binary)
	only := map[string]bool{name: true}
	changed := false
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			continue
		}
		names, err := enableInFile(f, extDir, only)
		if err != nil {
			return changed, err
		}
		if len(names) > 0 {
			changed = true
		}
	}
	return changed, nil
}

// ModuleAvailable reports whether the shared object for an extension is present
// in the extension_dir of the given PHP installation.
func ModuleAvailable(r ResolvedPHP, name string) bool {
	extDir := extensionDir(r.Binary)
	return extDir != "" && moduleExists(extDir, name)
}

// LoadedModules returns the lower-cased names of the modules reported by
// `php -m` for the given binary (including Zend extensions such as opcache).
func LoadedModules(binary string) ([]string, error) {
	if binary == "" {
		binary = "php"
	}
	out, err := exec.Command(binary, "-m").Output()
	if err != nil {
		return nil, fmt.Errorf("running %s -m: %w", binary, err)
	}
	var mods []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "[") {
			continue // section headers: [PHP Modules], [Zend Modules]
		}
		mods = append(mods, strings.ToLower(strings.ReplaceAll(line, " ", "")))
	}
	return mods, nil
}

// IsLoaded reports whether the named extension is loaded by the given binary.
func IsLoaded(binary, name string) bool {
	mods, err := LoadedModules(binary)
	if err != nil {
		return false
	}
	return contains(mods, normalizeModule(name))
}

// normalizeModule maps an extension name to how `php -m` spells it (lower-cased,
// spaces dropped), e.g. "Zend OPcache" → "zendopcache".
func normalizeModule(name string) string {
	name = strings.ToLower(name)
	if name == "opcache" {
		return "zendopcache"
	}
	return name
}

// scanDir returns the "additional .ini files" directory compiled into the given
// php binary (e.g. /etc/php/conf.d), or "" if there is none.
func scanDir(binary string) string {
	if binary == "" {
		binary = "php"
	}
	out, err := exec.Command(binary, "--ini").Output()
	if err != nil {
		return ""
	}
	for line := range strings.SplitSeq(string(out), "\n") {
		if _, after, ok := strings.Cut(line, "Scan for additional .ini files in:"); ok {
			dir := strings.TrimSpace(after)
			if dir == "(none)" {
				return ""
			}
			return dir
		}
	}
	return ""
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
