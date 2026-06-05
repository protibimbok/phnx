package php

import (
	"fmt"

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
