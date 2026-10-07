package initcmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/fpm"
	"github.com/protibimbok/phnx/internal/hosts"
	"github.com/protibimbok/phnx/internal/nginx"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	initPort      int
	initType      string
	initPHP       string
	initSubdomain string
)

// InitCmd is the `phnx init` command, registered by cmd/root.go.
var InitCmd = &cobra.Command{
	Use:   "init [dir]",
	Short: "Register a new local site",
	Long: `Register a directory as a local site.

The directory defaults to the current one. If it does not exist it is created.
The subdomain defaults to the directory name unless --subdomain is given.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runInit,
}

func init() {
	InitCmd.Flags().IntVar(&initPort, "port", 80, "Port to listen on")
	InitCmd.Flags().StringVar(&initType, "type", "", "Site type: laravel, wordpress, php")
	InitCmd.Flags().StringVar(&initPHP, "php", "", "PHP version to use")
	InitCmd.Flags().StringVar(&initSubdomain, "subdomain", "", "Subdomain to register (defaults to the directory name)")
}

func runInit(_ *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	cwd, err := resolveSiteDir(args)
	if err != nil {
		return err
	}

	defaultName := sanitizeSubdomain(filepath.Base(cwd))
	subdomain := initSubdomain
	if subdomain == "" {
		subdomain, err = ui.AskText("Subdomain", defaultName, defaultName)
		if err != nil {
			return err
		}
	}
	subdomain = sanitizeSubdomain(subdomain)
	domain := subdomain + "." + cfg.TLD

	phpVersion := resolvePhpVersion(cwd, cfg)
	resolved, err := php.ResolvePHP(cfg, phpVersion)
	if err != nil {
		return err
	}

	if !resolved.Tagged && initPHP == "" {
		_, fileErr := os.ReadFile(filepath.Join(cwd, ".php-version"))
		if fileErr != nil {
			ui.Warn(fmt.Sprintf("Using system PHP (%s, untagged). To use a specific version run: phnx php install <version>", phpVersion))
		}
	}

	siteType := initType
	if siteType == "" {
		siteType, err = ui.AskSelect("Site type", []string{"laravel", "wordpress", "php"})
		if err != nil {
			return err
		}
	}

	if cfg.FindSite(subdomain) != nil {
		return fmt.Errorf("subdomain %q is already registered", subdomain)
	}
	if exists, _ := hosts.HasEntry(domain); exists {
		return fmt.Errorf("domain %s already exists in /etc/hosts", domain)
	}

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

	if err := maybeScaffold(siteType, cwd, subdomain, resolved.Binary, cfg); err != nil {
		return err
	}

	if err := fpm.EnsureRunning(resolved.Service); err != nil {
		return err
	}

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

	if err := hosts.Add(domain); err != nil {
		return fmt.Errorf("updating /etc/hosts: %w", err)
	}
	ui.Success(fmt.Sprintf("Added %s to /etc/hosts", domain))

	if err := nginx.Reload(); err != nil {
		_ = hosts.Remove(domain)
		_ = nginx.RemoveSiteConfig(cfg.NginxSitesDir, subdomain)
		return fmt.Errorf("nginx reload failed — rolled back: %w", err)
	}
	ui.Success("nginx reloaded")

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

	ui.Separator()
	url := cfg.SiteURL(subdomain)
	if url == "" {
		url = fmt.Sprintf("http://%s", domain)
	}
	ui.Success(fmt.Sprintf("Site ready: %s", url))
	ui.Info(fmt.Sprintf("PHP: %s | Type: %s | Path: %s", phpVersion, siteType, cwd))
	return nil
}

func resolveSiteDir(args []string) (string, error) {
	dir := "."
	if len(args) > 0 && args[0] != "" {
		dir = args[0]
	}

	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("resolving directory %q: %w", dir, err)
	}

	if len(args) == 0 {
		ui.Warn(fmt.Sprintf("Using %s", abs))
	}

	info, err := os.Stat(abs)
	switch {
	case err == nil:
		if !info.IsDir() {
			return "", fmt.Errorf("%s is not a directory", abs)
		}
	case os.IsNotExist(err):
		if err := os.MkdirAll(abs, 0o755); err != nil {
			return "", fmt.Errorf("creating directory %s: %w", abs, err)
		}
		ui.Info(fmt.Sprintf("Created directory %s", abs))
	default:
		return "", fmt.Errorf("checking directory %s: %w", abs, err)
	}

	return abs, nil
}

func resolvePhpVersion(cwd string, cfg *config.Config) string {
	if initPHP != "" {
		return initPHP
	}

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
