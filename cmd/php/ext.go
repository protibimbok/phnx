package phpcmd

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/system"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var extPHP string

// pkgPrefixRe matches a package-style prefix: "php-", "php83-", "php8.3-", "php8.3.1-".
var pkgPrefixRe = regexp.MustCompile(`^php[0-9.]*-`)

var extCmd = &cobra.Command{
	Use:   "ext",
	Short: "Manage PHP extensions",
}

var extInstallCmd = &cobra.Command{
	Use:   "install <extension> [extension...]",
	Short: "Install PHP extensions for the project's PHP version (falls back to the default)",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runExtInstall,
}

func init() {
	extInstallCmd.Flags().StringVar(&extPHP, "php", "", "PHP version to install into (overrides project/default)")
	extCmd.AddCommand(extInstallCmd)
	PHPCmd.AddCommand(extCmd)
}

func runExtInstall(_ *cobra.Command, args []string) error {
	exts := make([]string, 0, len(args))
	for _, a := range args {
		ext := strings.ToLower(strings.TrimSpace(a))
		// Accept package-style names, e.g. "php-gd" / "php8.3-gd" → "gd".
		ext = pkgPrefixRe.ReplaceAllString(ext, "")
		if err := php.ValidateExtension(ext); err != nil {
			return err
		}
		exts = append(exts, ext)
	}

	if extPHP != "" {
		if err := php.ValidateVersion(extPHP); err != nil {
			return err
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("getting cwd: %w", err)
	}

	version, source, err := php.ResolveProjectVersion(cfg, cwd, extPHP)
	if err != nil {
		return err
	}
	resolved, err := php.ResolvePHP(cfg, version)
	if err != nil {
		return err
	}
	ui.Info(fmt.Sprintf("Using PHP %s (from %s)", version, source))

	var installed, failed []string
	for _, ext := range exts {
		if php.IsLoaded(resolved.Binary, ext) {
			ui.Info(fmt.Sprintf("%s is already enabled for PHP %s", ext, version))
			continue
		}

		// Bundled but disabled: the module is already on disk, just enable it.
		if !php.ModuleAvailable(resolved, ext) {
			if err := php.InstallExtension(resolved, ext); err != nil {
				ui.Error(fmt.Sprintf("installing %s: %v", ext, err))
				failed = append(failed, ext)
				continue
			}
		}

		if !php.IsLoaded(resolved.Binary, ext) {
			if _, err := php.EnableExtension(resolved, ext); err != nil {
				ui.Warn(fmt.Sprintf("enabling %s: %v", ext, err))
			}
		}

		if !php.IsLoaded(resolved.Binary, ext) {
			ui.Warn(fmt.Sprintf("PHP %s does not report %s as loaded — check its .ini", version, ext))
			failed = append(failed, ext)
			continue
		}
		ui.Success(fmt.Sprintf("%s enabled for PHP %s", ext, version))
		installed = append(installed, ext)
	}

	if len(installed) > 0 {
		svc := system.NewServiceManager()
		if err := svc.Restart(resolved.Service); err != nil {
			return fmt.Errorf("restarting %s: %w", resolved.Service, err)
		}
		ui.Success(fmt.Sprintf("%s restarted — enabled: %s", resolved.Service, strings.Join(installed, ", ")))
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to enable: %s", strings.Join(failed, ", "))
	}
	return nil
}
