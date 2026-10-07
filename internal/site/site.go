// Package site holds helpers for registering and removing phnx-managed sites
// that are shared between the init, remove and setup commands.
package site

import (
	"path/filepath"
	"time"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/hosts"
	"github.com/protibimbok/phnx/internal/nginx"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/system"
)

// RegisterInternal registers an internal phnx-managed site (called from setup subcommands).
func RegisterInternal(subdomain, path, siteType, phpVersion string, port int) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if cfg.FindSite(subdomain) != nil {
		return nil // already registered
	}

	resolved, err := php.ResolvePHP(cfg, phpVersion)
	if err != nil {
		return err
	}

	domain := subdomain + "." + cfg.TLD
	tmplData := nginx.TemplateData{
		Port:          port,
		ServerName:    domain,
		RootDir:       path,
		SiteName:      subdomain,
		PHPVersion:    phpVersion,
		FastcgiSocket: resolved.Socket,
	}
	if err := nginx.WriteSiteConfig(cfg.NginxSitesDir, subdomain, siteType, tmplData); err != nil {
		return err
	}
	if err := hosts.Add(domain); err != nil {
		return err
	}
	if err := nginx.Reload(); err != nil {
		return err
	}

	cfg.AddSite(config.Site{
		Subdomain: subdomain,
		Path:      path,
		Type:      siteType,
		PHP:       phpVersion,
		Port:      port,
		Internal:  true,
		CreatedAt: time.Now(),
	})
	return config.Save(cfg)
}

// Deregister removes a site's nginx config, hosts entry, and config entry.
func Deregister(subdomain string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	domain := subdomain + "." + cfg.TLD
	_ = nginx.RemoveSiteConfig(cfg.NginxSitesDir, subdomain)
	_ = hosts.Remove(domain)
	_ = nginx.Reload()
	cfg.RemoveSite(subdomain)
	return config.Save(cfg)
}

// RemoveLogFiles removes nginx log files for a site including archived ones.
func RemoveLogFiles(subdomain string) error {
	logDir := system.Platform.NginxLogDir
	patterns := []string{
		filepath.Join(logDir, subdomain+".access.log"),
		filepath.Join(logDir, subdomain+".error.log"),
		filepath.Join(logDir, subdomain+".access.log.*"),
		filepath.Join(logDir, subdomain+".error.log.*"),
	}
	for _, pattern := range patterns {
		matches, err := filepath.Glob(pattern)
		if err != nil {
			continue
		}
		for _, match := range matches {
			_ = system.RemoveFile(match)
		}
	}
	return nil
}
