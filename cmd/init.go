package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/fpm"
	"github.com/protibimbok/phnx/internal/hosts"
	"github.com/protibimbok/phnx/internal/nginx"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	initPort int
	initType string
	initPHP  string
)

var initCmd = &cobra.Command{
	Use:   "init [subdomain]",
	Short: "Register a new local site",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runInit,
}

func init() {
	rootCmd.AddCommand(initCmd)
	initCmd.Flags().IntVar(&initPort, "port", 80, "Port to listen on")
	initCmd.Flags().StringVar(&initType, "type", "", "Site type: laravel, wordpress, php")
	initCmd.Flags().StringVar(&initPHP, "php", "", "PHP version to use")
}

func runInit(_ *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting cwd: %w", err)
	}

	// 1. Resolve subdomain. A subdomain is always required; when not given as
	// an argument, prompt with the directory name as the default.
	defaultName := sanitizeSubdomain(filepath.Base(cwd))
	subdomain := ""
	if len(args) > 0 {
		subdomain = args[0]
	} else {
		subdomain, err = ui.AskText("Subdomain", defaultName, defaultName)
		if err != nil {
			return err
		}
	}
	subdomain = sanitizeSubdomain(subdomain)
	domain := subdomain + "." + cfg.TLD

	// 2. Resolve PHP version: .php-version file → --php flag → config default
	phpVersion := resolvePhpVersion(cwd, cfg)
	resolved, err := php.ResolvePHP(cfg, phpVersion)
	if err != nil {
		return err
	}
	// Warn when falling back to an untagged system PHP with no explicit pin.
	if !resolved.Tagged && initPHP == "" {
		_, fileErr := os.ReadFile(filepath.Join(cwd, ".php-version"))
		if fileErr != nil {
			ui.Warn(fmt.Sprintf("Using system PHP (%s, untagged). To use a specific version run: phnx php install <version>", phpVersion))
		}
	}

	// 3. Resolve type
	siteType := initType
	if siteType == "" {
		siteType, err = ui.AskSelect("Site type", []string{"laravel", "wordpress", "php"})
		if err != nil {
			return err
		}
	}

	// 4. Check subdomain not already in use
	if cfg.FindSite(subdomain) != nil {
		return fmt.Errorf("subdomain %q is already registered", subdomain)
	}
	if exists, _ := hosts.HasEntry(domain); exists {
		return fmt.Errorf("domain %s already exists in /etc/hosts", domain)
	}

	// 5. Check port not already in use.
	// Sites default to 80 (and 443) and are served via name-based virtual
	// hosting, so many sites can share those ports with different domains.
	// A port conflict only matters when a special, explicit port is requested
	// (e.g. remote view subdomains served locally).
	if initPort != 80 && initPort != 443 {
		for _, s := range cfg.Sites {
			if s.Port == initPort && s.Subdomain != subdomain {
				return fmt.Errorf("port %d is already used by site %q", initPort, s.Subdomain)
			}
		}
	}

	ui.Header(fmt.Sprintf("Initializing %s", domain))
	ui.Info(fmt.Sprintf("Path: %s", cwd))
	ui.Info(fmt.Sprintf("Type: %s | PHP: %s | Port: %d", siteType, phpVersion, initPort))

	// 6. Laravel scaffolding
	if siteType == "laravel" && isDirEmpty(cwd) {
		scaffold, _ := ui.Confirm("Directory is empty. Create a new Laravel project?", true)
		if scaffold {
			ui.Info("Running composer create-project laravel/laravel ...")
			cmd := exec.Command("composer", "create-project", "laravel/laravel", ".")
			cmd.Dir = cwd
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err != nil {
				return fmt.Errorf("composer create-project: %w", err)
			}
		}
	}

	// 7. WordPress scaffolding
	if siteType == "wordpress" && isDirEmpty(cwd) {
		if err := scaffoldWordPress(cwd, cfg); err != nil {
			return err
		}
	}

	// 8. Ensure FPM is running for chosen PHP version
	if err := fpm.EnsureRunning(resolved.Service); err != nil {
		return err
	}

	// 9. Write nginx config
	tmplData := nginx.TemplateData{
		Port:          initPort,
		ServerName:    domain,
		RootDir:       cwd,
		SiteName:      subdomain,
		PHPVersion:    phpVersion,
		FastcgiSocket: resolved.Socket,
	}
	if err := nginx.WriteSiteConfig(cfg.NginxSitesDir, subdomain, siteType, tmplData); err != nil {
		return fmt.Errorf("writing nginx config: %w", err)
	}
	ui.Success(fmt.Sprintf("nginx config written: %s/%s.conf", cfg.NginxSitesDir, subdomain))

	// 10. Add /etc/hosts entry
	if err := hosts.Add(domain); err != nil {
		return fmt.Errorf("updating /etc/hosts: %w", err)
	}
	ui.Success(fmt.Sprintf("Added %s to /etc/hosts", domain))

	// 11. Test and reload nginx
	if err := nginx.Reload(); err != nil {
		_ = hosts.Remove(domain)
		_ = nginx.RemoveSiteConfig(cfg.NginxSitesDir, subdomain)
		return fmt.Errorf("nginx reload failed — rolled back: %w", err)
	}
	ui.Success("nginx reloaded")

	// 12. Save to config.json
	site := config.Site{
		Subdomain: subdomain,
		Path:      cwd,
		Type:      siteType,
		PHP:       phpVersion,
		Port:      initPort,
		Internal:  false,
		CreatedAt: time.Now(),
	}
	cfg.AddSite(site)
	if err := config.Save(cfg); err != nil {
		return err
	}

	// 13. Print result
	ui.Separator()
	url := cfg.SiteURL(subdomain)
	if url == "" {
		url = fmt.Sprintf("http://%s", domain)
	}
	ui.Success(fmt.Sprintf("Site ready: %s", url))
	ui.Info(fmt.Sprintf("PHP: %s | Type: %s | Path: %s", phpVersion, siteType, cwd))
	return nil
}

func resolvePhpVersion(cwd string, cfg *config.Config) string {
	if initPHP != "" {
		return initPHP
	}
	// Check .php-version file
	data, err := os.ReadFile(filepath.Join(cwd, ".php-version"))
	if err == nil {
		v := strings.TrimSpace(string(data))
		if v != "" {
			return v
		}
	}
	return cfg.DefaultPHP
}

func sanitizeSubdomain(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, "_", "-")
	s = strings.ReplaceAll(s, " ", "-")
	return s
}

func isDirEmpty(path string) bool {
	entries, err := os.ReadDir(path)
	if err != nil {
		return false
	}
	return len(entries) == 0
}

func scaffoldWordPress(cwd string, cfg *config.Config) error {
	ui.Info("Downloading WordPress...")

	// Download latest WordPress
	wpCmd := exec.Command("bash", "-c",
		fmt.Sprintf(`cd %q && curl -O https://wordpress.org/latest.zip && unzip -q latest.zip && mv wordpress/* . && rmdir wordpress && rm latest.zip`, cwd))
	wpCmd.Stdout = os.Stdout
	wpCmd.Stderr = os.Stderr
	if err := wpCmd.Run(); err != nil {
		return fmt.Errorf("downloading WordPress: %w", err)
	}

	dbName, err := ui.AskText("Database name", "wordpress", "wordpress")
	if err != nil {
		return err
	}

	// Create database
	createDB := fmt.Sprintf(
		"mysql -h %s -P %d -u %s -p%s -e \"CREATE DATABASE IF NOT EXISTS `%s`;\"",
		cfg.MySQL.Host, cfg.MySQL.Port, cfg.MySQL.User, cfg.MySQL.Password, dbName,
	)
	if out, err := exec.Command("bash", "-c", createDB).CombinedOutput(); err != nil {
		ui.Warn(fmt.Sprintf("Could not create database %s: %v\n%s", dbName, err, string(out)))
	} else {
		ui.Success(fmt.Sprintf("Database %s created", dbName))
	}

	// Run wp config create if wpcli available
	if _, err := exec.LookPath("wp"); err == nil {
		wpConfig := exec.Command("wp", "config", "create",
			fmt.Sprintf("--dbname=%s", dbName),
			fmt.Sprintf("--dbuser=%s", cfg.MySQL.User),
			fmt.Sprintf("--dbpass=%s", cfg.MySQL.Password),
			fmt.Sprintf("--dbhost=%s:%d", cfg.MySQL.Host, cfg.MySQL.Port),
		)
		wpConfig.Dir = cwd
		wpConfig.Stdout = os.Stdout
		wpConfig.Stderr = os.Stderr
		if err := wpConfig.Run(); err != nil {
			ui.Warn(fmt.Sprintf("wp config create failed: %v", err))
		} else {
			ui.Success("wp-config.php created")
		}
	}

	return nil
}

// RegisterInternalSite registers an internal phnx-managed site (called from setup subcommands).
func RegisterInternalSite(subdomain, path, siteType, phpVersion string, port int) error {
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

// DeregisterSite removes a site's nginx config, hosts entry, and config entry.
func DeregisterSite(subdomain string) error {
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
