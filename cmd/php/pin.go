package phpcmd

import (
	"fmt"
	"os"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/nginx"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var pinCmd = &cobra.Command{
	Use:   "pin <version>",
	Short: "Pin the PHP version for the current project",
	Args:  cobra.ExactArgs(1),
	RunE:  runPin,
}

func init() {
	PHPCmd.AddCommand(pinCmd)
}

func runPin(_ *cobra.Command, args []string) error {
	version := args[0]
	if err := php.ValidateVersion(version); err != nil {
		return err
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if !cfg.HasPHPVersion(version) {
		return fmt.Errorf("PHP %s is not installed — run 'phnx php install %s' first", version, version)
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting cwd: %w", err)
	}

	// Write .php-version file
	if err := os.WriteFile(".php-version", []byte(version+"\n"), 0644); err != nil {
		return fmt.Errorf("writing .php-version: %w", err)
	}
	ui.Success(fmt.Sprintf("Wrote .php-version: %s", version))

	// Find site in config matching cwd
	site := cfg.FindSiteByPath(cwd)
	if site == nil {
		ui.Info("No site registered for this directory — nginx config not updated.")
		return nil
	}

	if site.PHP == version {
		ui.Info(fmt.Sprintf("Site %s is already using PHP %s", site.Subdomain, version))
		return nil
	}

	site.PHP = version
	if err := config.Save(cfg); err != nil {
		return err
	}

	// Regenerate nginx config
	resolved, err := php.ResolvePHP(cfg, version)
	if err != nil {
		return err
	}
	domain := site.Subdomain + "." + cfg.TLD
	tmplData := nginx.TemplateData{
		Port:          site.Port,
		ServerName:    domain,
		RootDir:       site.Path,
		SiteName:      site.Subdomain,
		PHPVersion:    version,
		FastcgiSocket: resolved.Socket,
	}
	if err := nginx.WriteSiteConfig(cfg.NginxSitesDir, site.Subdomain, site.Type, tmplData); err != nil {
		return fmt.Errorf("regenerating nginx config: %w", err)
	}

	if err := nginx.Reload(); err != nil {
		return fmt.Errorf("nginx reload: %w", err)
	}

	ui.Success(fmt.Sprintf("Site %s pinned to PHP %s and nginx reloaded", site.Subdomain, version))
	return nil
}
