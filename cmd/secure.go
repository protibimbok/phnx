package cmd

import (
	"fmt"
	"os"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/nginx"
	sitepkg "github.com/protibimbok/phnx/internal/site"
	"github.com/protibimbok/phnx/internal/ssl"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var secureRenew bool

var secureCmd = &cobra.Command{
	Use:   "secure [subdomain]",
	Short: "Serve a site over HTTPS with a locally trusted certificate",
	Long: `Issue a certificate for a site and switch its nginx config to HTTPS.

On first use phnx creates a local certificate authority in ~/.phnx/ssl and
installs it into the system trust store (and Firefox/Chrome databases when
certutil is available), so browsers show a valid padlock for https://<site>.<tld>.

A site that listened on port 80 moves to 443, and plain-HTTP requests are
redirected to HTTPS. Sites on a custom port keep that port.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runSecure,
}

var unsecureCmd = &cobra.Command{
	Use:   "unsecure [subdomain]",
	Short: "Switch a site back to plain HTTP",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runUnsecure,
}

func init() {
	rootCmd.AddCommand(secureCmd)
	rootCmd.AddCommand(unsecureCmd)
	secureCmd.Flags().BoolVar(&secureRenew, "renew", false, "Re-issue the certificate even if the site is already secure")
}

func runSecure(_ *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	site, err := resolveSite(cfg, args)
	if err != nil {
		return err
	}
	domain := cfg.SitesDomain(site.Subdomain)

	if site.Secure && !secureRenew && ssl.CertExists(site.Subdomain) {
		ui.Info(fmt.Sprintf("%s is already served over HTTPS: %s", domain, cfg.SiteURL(site.Subdomain)))
		ui.Info("Use --renew to issue a fresh certificate.")
		return nil
	}

	ui.Header(fmt.Sprintf("Securing %s", domain))

	if err := sitepkg.EnsureCertificate(site.Subdomain, domain); err != nil {
		return err
	}

	previous := *site
	site.Secure = true
	if site.Port == 80 {
		site.Port = 443
	}

	if err := applySiteConfig(cfg, site, previous); err != nil {
		return err
	}

	ui.Separator()
	ui.Success(fmt.Sprintf("Site ready: %s", cfg.SiteURL(site.Subdomain)))
	return nil
}

func runUnsecure(_ *cobra.Command, args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	site, err := resolveSite(cfg, args)
	if err != nil {
		return err
	}
	domain := cfg.SitesDomain(site.Subdomain)

	if !site.Secure {
		ui.Info(fmt.Sprintf("%s is already served over plain HTTP", domain))
		return nil
	}

	ui.Header(fmt.Sprintf("Switching %s back to HTTP", domain))

	previous := *site
	site.Secure = false
	if site.Port == 443 {
		site.Port = 80
	}

	if err := applySiteConfig(cfg, site, previous); err != nil {
		return err
	}

	if err := ssl.RemoveCert(site.Subdomain); err != nil {
		ui.Warn(fmt.Sprintf("removing certificate: %v", err))
	} else {
		ui.Success("Certificate removed")
	}

	ui.Separator()
	ui.Success(fmt.Sprintf("Site ready: %s", cfg.SiteURL(site.Subdomain)))
	return nil
}

func applySiteConfig(cfg *config.Config, site *config.Site, previous config.Site) error {
	if err := sitepkg.WriteNginxConfig(cfg, *site); err != nil {
		return fmt.Errorf("writing nginx config: %w", err)
	}
	ui.Success(fmt.Sprintf("nginx config written: %s", nginx.SiteConfigPath(cfg.NginxSitesDir, site.Subdomain)))

	if err := nginx.Reload(); err != nil {
		_ = sitepkg.WriteNginxConfig(cfg, previous)
		_ = nginx.Reload()
		*site = previous
		return fmt.Errorf("nginx reload failed — rolled back: %w", err)
	}
	ui.Success("nginx reloaded")

	return config.Save(cfg)
}

func resolveSite(cfg *config.Config, args []string) (*config.Site, error) {
	if len(args) > 0 {
		site := cfg.FindSite(args[0])
		if site == nil {
			return nil, fmt.Errorf("site %q not found", args[0])
		}
		return site, nil
	}
	cwd, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("getting cwd: %w", err)
	}
	site := cfg.FindSiteByPath(cwd)
	if site == nil {
		return nil, fmt.Errorf("no phnx site registered for current directory — pass a subdomain as argument")
	}
	return site, nil
}
