package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/fpm"
	"github.com/protibimbok/phnx/internal/nginx"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var configureCmd = &cobra.Command{
	Use:   "configure",
	Short: "Set up phnx for the first time (idempotent)",
	RunE:  runConfigure,
}

func init() {
	rootCmd.AddCommand(configureCmd)
}

func runConfigure(_ *cobra.Command, _ []string) error {
	// 1. Resolve real user (reject root)
	realUser, err := system.RealUser()
	if err != nil {
		return err
	}
	realGroup, err := system.PrimaryGroup(realUser)
	if err != nil {
		return err
	}

	ui.Header("phnx configure")
	ui.Info(fmt.Sprintf("Running as: %s (%s)", realUser.Username, realGroup))

	// 2. Detect nginx
	nginxDir := system.Platform.NginxDir
	if _, err := os.Stat(nginxDir); os.IsNotExist(err) {
		return fmt.Errorf("nginx not found at %s — install nginx first", nginxDir)
	}
	ui.Success(fmt.Sprintf("nginx found at %s", nginxDir))

	// 3. Load or create config
	cfg, err := config.Load()
	if err != nil {
		cfg = &config.Config{
			TLD:           "test",
			NginxDir:      nginxDir,
			NginxSitesDir: filepath.Join(nginxDir, "phnx-sites"),
			DefaultPHP:    "",
			RealUser:      realUser.Username,
			RealGroup:     realGroup,
			MySQL: config.MySQLConfig{
				Host:     "127.0.0.1",
				Port:     3306,
				User:     "phnx",
				Password: "secret",
			},
		}
	}
	cfg.RealUser = realUser.Username
	cfg.RealGroup = realGroup
	cfg.NginxDir = nginxDir

	// 4. Prompt TLD
	tld, err := ui.AskText("Preferred TLD (e.g. test, local)", "test", cfg.TLD)
	if err != nil {
		return err
	}
	if tld != "" {
		cfg.TLD = tld
	}

	// 5. Prompt default PHP version
	installed, _ := php.DetectInstalled() // tagged packages only (phpXX) on Arch
	if len(installed) > 0 {
		ui.Info(fmt.Sprintf("Installed PHP versions: %v", installed))
		phpVer, err := ui.AskSelect("Default PHP version", installed)
		if err != nil {
			return err
		}
		cfg.DefaultPHP = phpVer
		cfg.PHPVersions = nil
		for _, v := range installed {
			cfg.AddPHPVersion(config.PHPVersion{Version: v, Tagged: true})
		}
	} else if system.IsArch() {
		// No tagged PHP on Arch — check for the untagged system 'php-fpm' package.
		if systemVer := php.DetectArchSystemPHP(); systemVer != "" {
			ui.Info(fmt.Sprintf("System PHP %s detected (php-fpm package)", systemVer))
			cfg.DefaultPHP = systemVer
			cfg.PHPVersions = nil // clear any stale tagged entry from a previous configure run
			cfg.AddPHPVersion(config.PHPVersion{
				Version: systemVer,
				Tagged:  false,
				Socket:  "/run/php-fpm/php-fpm.sock",
				Service: "php-fpm",
				Binary:  "/usr/bin/php",
			})
		} else {
			ui.Warn("No PHP-FPM detected.")
			install, err := ui.Confirm("Install system PHP now?", true)
			if err != nil {
				return err
			}
			if install {
				if err := system.Run("pacman", "-Syu", "--noconfirm", "php", "php-fpm"); err != nil {
					return err
				}
				out, err := system.OutputUser("php", "-r", "echo PHP_MAJOR_VERSION.'.'.PHP_MINOR_VERSION;")
				if err != nil {
					return fmt.Errorf("detecting PHP version: %w", err)
				}
				detected := strings.TrimSpace(out)
				cfg.DefaultPHP = detected
				cfg.PHPVersions = nil // clear any stale entries
				cfg.AddPHPVersion(config.PHPVersion{
					Version: detected,
					Tagged:  false,
					Socket:  "/run/php-fpm/php-fpm.sock",
					Service: "php-fpm",
					Binary:  "/usr/bin/php",
				})
			}
		}
	} else {
		ui.Warn("No PHP-FPM versions detected.")
		latest := php.LatestStable()
		install, err := ui.Confirm(fmt.Sprintf("Install PHP %s now?", latest), true)
		if err != nil {
			return err
		}
		if install {
			if err := php.EnsurePPA(); err != nil {
				return err
			}
			if err := php.Install(latest); err != nil {
				return err
			}
			cfg.DefaultPHP = latest
			cfg.AddPHPVersion(config.PHPVersion{Version: latest, Tagged: true})
		}
	}

	// 6. Create config dir
	if err := os.MkdirAll(config.Dir(), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	// 7. Create nginx phnx-sites dir (sudo)
	sitesDir := filepath.Join(nginxDir, "phnx-sites")
	cfg.NginxSitesDir = sitesDir
	if err := system.MkdirAll(sitesDir); err != nil {
		return fmt.Errorf("creating nginx sites dir: %w", err)
	}
	ui.Success(fmt.Sprintf("nginx sites dir: %s", sitesDir))

	// 8. Add include directive to nginx.conf
	if err := nginx.EnsureSitesDirIncluded(nginxDir, sitesDir); err != nil {
		ui.Warn(fmt.Sprintf("Could not auto-add include directive: %v", err))
		ui.Info(fmt.Sprintf("Please manually add to your nginx.conf http block:\n  include %s/*.conf;", sitesDir))
	}

	// 9. Set nginx worker user so workers can access project directories.
	// On Linux the default nginx user (http/www-data) cannot read WSL or
	// user-owned paths; running as the real user fixes that.
	if runtime.GOOS == "linux" {
		webGroup := system.NginxWebUser()
		if err := nginx.EnsureWorkerUser(nginxDir, realUser.Username, webGroup); err != nil {
			ui.Warn(fmt.Sprintf("Could not set nginx worker user: %v", err))
		} else {
			ui.Success(fmt.Sprintf("nginx worker user set to %s %s", realUser.Username, webGroup))
		}
	}

	// 10. Add ondrej PPA / shivammathur tap (not needed on Arch — handled per-install)
	if !system.IsArch() {
		if err := php.EnsurePPA(); err != nil {
			ui.Warn(fmt.Sprintf("PPA setup: %v", err))
		}
	}

	// 11. Write FPM pool config for default PHP (tagged only; untagged uses distro pool)
	if cfg.DefaultPHP != "" {
		resolved, resolveErr := php.ResolvePHP(cfg, cfg.DefaultPHP)
		if resolveErr != nil {
			ui.Warn(fmt.Sprintf("could not resolve PHP %s: %v", cfg.DefaultPHP, resolveErr))
		} else {
			// Enable bundled extensions in php.ini before the FPM service is
			// (re)started below so the new modules are picked up.
			enablePHPExtensions(resolved)

			// Expose the default PHP as the `php` command. On Fedora the Remi
			// SCL binary isn't on PATH, so without this `php` is not found.
			if err := php.LinkDefaultBinary(resolved.Binary); err != nil {
				ui.Warn(fmt.Sprintf("Could not link php command: %v", err))
			} else {
				ui.Success(fmt.Sprintf("php command linked to /usr/local/bin/php (PHP %s)", resolved.Version))
			}

			if resolved.Tagged {
				if err := fpm.WritePool(cfg.DefaultPHP, cfg.RealUser, cfg.RealGroup); err != nil {
					return fmt.Errorf("writing FPM pool: %w", err)
				}
				svc := system.NewServiceManager()
				_ = svc.Restart(resolved.Service)
				ui.Success(fmt.Sprintf("FPM pool written for PHP %s", cfg.DefaultPHP))
			} else {
				// Untagged/system php-fpm uses the distro pool (e.g. www.conf), which
				// runs as http/www-data. Make it run as the real user so workers can
				// read project roots under user-owned, non-world-traversable paths.
				if runtime.GOOS == "linux" {
					if poolFile, perr := fpm.EnsurePoolRunsAsUser(resolved.Socket, cfg.RealUser, cfg.RealGroup); perr != nil {
						ui.Warn(fmt.Sprintf("Could not set php-fpm pool user: %v", perr))
					} else {
						ui.Success(fmt.Sprintf("php-fpm pool user set to %s %s in %s", cfg.RealUser, cfg.RealGroup, poolFile))
					}
				}
				svc := system.NewServiceManager()
				_ = svc.Enable(resolved.Service)
				_ = svc.Restart(resolved.Service)
				ui.Success(fmt.Sprintf("System php-fpm service started for PHP %s", cfg.DefaultPHP))
			}
		}
	}

	// 12. Add user to nginx web group (Linux only) — needed so the real user
	// can connect to PHP-FPM sockets owned by the web group.
	if runtime.GOOS == "linux" {
		webGroup := system.NginxWebUser()
		if err := system.Run("usermod", "-aG", webGroup, realUser.Username); err != nil {
			ui.Warn(fmt.Sprintf("Could not add %s to %s group: %v", realUser.Username, webGroup, err))
		} else {
			ui.Warn(fmt.Sprintf("Added %s to %s group — please log out and back in for this to take effect.", realUser.Username, webGroup))
		}
	}

	// 13. Save config
	if err := config.Save(cfg); err != nil {
		return err
	}
	ui.Success(fmt.Sprintf("Config saved to %s", config.Path()))

	// 14. Test and reload nginx
	if err := nginx.Reload(); err != nil {
		ui.Warn(fmt.Sprintf("nginx reload: %v", err))
	} else {
		ui.Success("nginx reloaded")
	}

	ui.Separator()
	ui.Success("phnx is configured and ready!")
	return nil
}

// enablePHPExtensions uncomments the bundled extensions in the resolved PHP's
// php.ini file(s). Failures are non-fatal — they only mean some extensions stay
// disabled, which the user can fix manually.
func enablePHPExtensions(resolved php.ResolvedPHP) {
	names, err := php.EnableExtensions(resolved)
	if err != nil {
		ui.Warn(fmt.Sprintf("could not enable PHP extensions: %v", err))
		return
	}
	if len(names) > 0 {
		ui.Success(fmt.Sprintf("Enabled %d PHP extension(s) for PHP %s: %s",
			len(names), resolved.Version, strings.Join(names, ", ")))
	}
}
