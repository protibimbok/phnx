package php

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/system"
)

// ResolvedPHP holds the concrete socket/service/binary for one PHP installation.
type ResolvedPHP struct {
	Version string
	Tagged  bool
	Socket  string
	Service string
	Binary  string
}

// ResolvePHP is the single source of truth for socket/service/binary paths.
// Tagged=false (untagged system PHP): returns stored values verbatim.
// Tagged=true (named version): computes values via system.Platform functions.
func ResolvePHP(cfg *config.Config, version string) (ResolvedPHP, error) {
	entry := cfg.FindPHPVersion(version)
	if entry == nil {
		return ResolvedPHP{}, fmt.Errorf("PHP %s is not registered — run 'phnx php install %s' first", version, version)
	}
	if !entry.Tagged {
		return ResolvedPHP{
			Version: entry.Version,
			Tagged:  false,
			Socket:  entry.Socket,
			Service: entry.Service,
			Binary:  entry.Binary,
		}, nil
	}
	return ResolvedPHP{
		Version: version,
		Tagged:  true,
		Socket:  system.Platform.PhpFpmSock(version),
		Service: system.PHPFPMServiceName(version),
		Binary:  system.Platform.PhpBinaryPath(version),
	}, nil
}

// LinkDefaultBinary points /usr/local/bin/php at the given PHP binary so the
// `php` command resolves to the default version. This matters most on Fedora,
// where Remi software-collection binaries live under /opt/remi and are never on
// PATH; on Debian/Arch it simply pins `php` to the chosen version.
func LinkDefaultBinary(binary string) error {
	return system.Run("ln", "-sf", binary, "/usr/local/bin/php")
}

// ResolveProjectVersion picks the PHP version for the project at cwd, in order of precedence:
// explicit override > .php-version file > registered site > the configured default (system) PHP.
func ResolveProjectVersion(cfg *config.Config, cwd, override string) (version, source string, err error) {
	if override != "" {
		return override, "--php flag", nil
	}
	if data, err := os.ReadFile(filepath.Join(cwd, ".php-version")); err == nil {
		if v := strings.TrimSpace(string(data)); v != "" {
			return v, ".php-version", nil
		}
	}
	if site := cfg.FindSiteByPath(cwd); site != nil && site.PHP != "" {
		return site.PHP, fmt.Sprintf("site %s", site.Subdomain), nil
	}
	if cfg.DefaultPHP != "" {
		return cfg.DefaultPHP, "system default", nil
	}
	return "", "", fmt.Errorf("no PHP version found — pass --php, run 'phnx php pin <version>', or set a default with 'phnx php default <version>'")
}
