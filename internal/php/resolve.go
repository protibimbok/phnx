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
