package php

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/protibimbok/phnx/internal/system"
)

var versionRe = regexp.MustCompile(`^(\d+)\.(\d+)$`)

// latestStableFallback is used when the php.net API is unreachable.
const latestStableFallback = "8.4"

var (
	latestOnce   sync.Once
	latestCached string
)

// LatestStable returns the latest stable PHP minor version (e.g. "8.4").
// It queries the php.net releases API on first call and caches the result.
// Falls back to a compile-time default if the network is unavailable.
func LatestStable() string {
	latestOnce.Do(func() {
		latestCached = fetchLatestStable()
	})
	return latestCached
}

func fetchLatestStable() string {
	client := &http.Client{Timeout: 4 * time.Second}
	resp, err := client.Get("https://www.php.net/releases/index.php?json")
	if err != nil {
		return latestStableFallback
	}
	defer resp.Body.Close()

	var releases map[string]json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&releases); err != nil {
		return latestStableFallback
	}

	best := ""
	for key := range releases {
		// Key may be "8.4" or "8.4.7" — extract major.minor
		parts := strings.SplitN(key, ".", 3)
		if len(parts) < 2 {
			continue
		}
		mv := parts[0] + "." + parts[1]
		if !versionRe.MatchString(mv) {
			continue
		}
		if best == "" || versionGreater(mv, best) {
			best = mv
		}
	}
	if best == "" {
		return latestStableFallback
	}
	return best
}

func versionGreater(a, b string) bool {
	ap := strings.SplitN(a, ".", 2)
	bp := strings.SplitN(b, ".", 2)
	aMaj, _ := strconv.Atoi(ap[0])
	bMaj, _ := strconv.Atoi(bp[0])
	if aMaj != bMaj {
		return aMaj > bMaj
	}
	aMin, _ := strconv.Atoi(ap[1])
	bMin, _ := strconv.Atoi(bp[1])
	return aMin > bMin
}

// ValidateVersion checks that a version string is in "major.minor" form (e.g. "8.2").
func ValidateVersion(v string) error {
	if !versionRe.MatchString(v) {
		return fmt.Errorf("invalid PHP version %q — expected format: 8.2", v)
	}
	return nil
}

// DetectInstalled returns PHP versions installed on the system.
func DetectInstalled() ([]string, error) {
	if runtime.GOOS == "darwin" {
		return detectMac()
	}
	switch {
	case system.IsArch():
		return detectArch()
	case system.IsDebian():
		return detectDebian()
	case system.IsFedora():
		return detectFedora()
	default:
		return detectGeneric()
	}
}

// fedoraFPMPkgRe matches Remi software-collection FPM package names, e.g.
// "php84-php-fpm" → tag "84".
var fedoraFPMPkgRe = regexp.MustCompile(`^php(\d+)-php-fpm$`)

// detectFedora lists installed Remi SCL php-fpm packages (php84-php-fpm, …) and
// maps the version tag back to major.minor (e.g. "84" → "8.4"). The SCL binaries
// live under /opt/remi and are not on PATH, so a PATH probe (detectGeneric) would
// miss them.
func detectFedora() ([]string, error) {
	out, err := exec.Command("bash", "-c",
		`rpm -qa --qf '%{NAME}\n' 'php*-php-fpm' 2>/dev/null`,
	).Output()
	if err != nil {
		return nil, nil
	}
	var versions []string
	for line := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		m := fedoraFPMPkgRe.FindStringSubmatch(strings.TrimSpace(line))
		if m == nil {
			continue
		}
		tag := m[1] // e.g. "84"
		if len(tag) < 2 {
			continue
		}
		v := tag[:len(tag)-1] + "." + tag[len(tag)-1:]
		if versionRe.MatchString(v) {
			versions = append(versions, v)
		}
	}
	return versions, nil
}

func detectDebian() ([]string, error) {
	out, err := exec.Command("bash", "-c",
		`dpkg -l 'php*-fpm' 2>/dev/null | grep '^ii' | awk '{print $2}' | sed 's/php//;s/-fpm//'`,
	).Output()
	if err != nil {
		return nil, nil
	}
	return parseVersionLines(string(out)), nil
}

func detectArch() ([]string, error) {
	// Only versioned AUR/archlinuxcn packages: php80, php81, php82, php83, php84 …
	// The untagged system 'php'/'php-fpm' package is handled separately via
	// DetectArchSystemPHP and registered as Tagged=false by configure.
	var versions []string
	out, err := exec.Command("bash", "-c",
		`pacman -Qq 2>/dev/null | grep -E '^php[0-9]{2}$'`,
	).Output()
	if err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			line = strings.TrimSpace(line)
			if len(line) == 5 && strings.HasPrefix(line, "php") {
				major := string(line[3])
				minor := string(line[4])
				versions = append(versions, major+"."+minor)
			}
		}
	}
	return versions, nil
}

// DetectArchSystemPHP returns the version of the untagged system PHP package
// (i.e. the plain 'php-fpm' pacman package) when running on Arch Linux.
// Returns "" if the package is not installed or not on Arch.
func DetectArchSystemPHP() string {
	if !system.IsArch() {
		return ""
	}
	out, err := exec.Command("pacman", "-Qq", "php-fpm").Output()
	if err != nil || strings.TrimSpace(string(out)) != "php-fpm" {
		return ""
	}
	return defaultPHPVersion()
}

func detectMac() ([]string, error) {
	var versions []string

	// shivammathur/php versioned: php@8.2, php@8.3 ...
	out, err := exec.Command("bash", "-c",
		`brew list --formula 2>/dev/null | grep '^php@' | sed 's/php@//'`,
	).Output()
	if err == nil {
		versions = append(versions, parseVersionLines(string(out))...)
	}

	// Default Homebrew php (brew install php — no @version suffix)
	if out2, err2 := exec.Command("brew", "list", "--formula", "php").Output(); err2 == nil {
		if strings.Contains(string(out2), "php") {
			if v := defaultPHPVersion(); v != "" && !contains(versions, v) {
				versions = append(versions, v)
			}
		}
	}

	return versions, nil
}

func detectGeneric() ([]string, error) {
	// Fallback: probe php binaries on PATH
	var versions []string
	out, _ := exec.Command("bash", "-c",
		`compgen -c 2>/dev/null | grep -E '^php[0-9]+\.[0-9]' | sed 's/php//' | sort -u`,
	).Output()
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		line = strings.TrimSpace(line)
		if versionRe.MatchString(line) {
			versions = append(versions, line)
		}
	}
	return versions, nil
}

func parseVersionLines(s string) []string {
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(s), "\n") {
		line = strings.TrimSpace(line)
		if versionRe.MatchString(line) {
			out = append(out, line)
		}
	}
	return out
}

// defaultPHPVersion returns the version reported by the default `php` binary.
func defaultPHPVersion() string {
	out, err := exec.Command("php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;").Output()
	if err != nil {
		return ""
	}
	v := strings.TrimSpace(string(out))
	if versionRe.MatchString(v) {
		return v
	}
	return ""
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}
