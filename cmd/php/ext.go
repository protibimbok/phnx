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

var extDisableCmd = &cobra.Command{
	Use:   "disable <extension> [extension...]",
	Short: "Disable PHP extensions in php.ini without uninstalling them",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runExtDisable,
}

var extUninstallCmd = &cobra.Command{
	Use:   "uninstall <extension> [extension...]",
	Short: "Remove the packages providing PHP extensions (bundled ones are disabled instead)",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runExtUninstall,
}

func init() {
	extCmd.PersistentFlags().StringVar(&extPHP, "php", "", "PHP version to target (overrides project/default)")
	extCmd.AddCommand(extInstallCmd, extDisableCmd, extUninstallCmd)
	PHPCmd.AddCommand(extCmd)
}

// resolveExtTarget normalises the extension arguments and resolves the PHP
// installation they apply to.
func resolveExtTarget(args []string) ([]string, php.ResolvedPHP, error) {
	exts := make([]string, 0, len(args))
	for _, a := range args {
		ext := strings.ToLower(strings.TrimSpace(a))
		// Accept package-style names, e.g. "php-gd" / "php8.3-gd" → "gd".
		ext = pkgPrefixRe.ReplaceAllString(ext, "")
		if err := php.ValidateExtension(ext); err != nil {
			return nil, php.ResolvedPHP{}, err
		}
		exts = append(exts, ext)
	}

	if extPHP != "" {
		if err := php.ValidateVersion(extPHP); err != nil {
			return nil, php.ResolvedPHP{}, err
		}
	}

	cfg, err := config.Load()
	if err != nil {
		return nil, php.ResolvedPHP{}, err
	}

	cwd, err := os.Getwd()
	if err != nil {
		return nil, php.ResolvedPHP{}, fmt.Errorf("getting cwd: %w", err)
	}

	version, source, err := php.ResolveProjectVersion(cfg, cwd, extPHP)
	if err != nil {
		return nil, php.ResolvedPHP{}, err
	}
	resolved, err := php.ResolvePHP(cfg, version)
	if err != nil {
		return nil, php.ResolvedPHP{}, err
	}
	ui.Info(fmt.Sprintf("Using PHP %s (from %s)", version, source))
	return exts, resolved, nil
}

// finishExt restarts FPM when anything changed and reports the failures.
func finishExt(resolved php.ResolvedPHP, verb string, done, failed []string) error {
	if len(done) > 0 {
		svc := system.NewServiceManager()
		if err := svc.Restart(resolved.Service); err != nil {
			return fmt.Errorf("restarting %s: %w", resolved.Service, err)
		}
		ui.Success(fmt.Sprintf("%s restarted — %s: %s", resolved.Service, verb, strings.Join(done, ", ")))
	}

	if len(failed) > 0 {
		return fmt.Errorf("failed to %s: %s", strings.TrimSuffix(verb, "d"), strings.Join(failed, ", "))
	}
	return nil
}

func runExtInstall(_ *cobra.Command, args []string) error {
	exts, resolved, err := resolveExtTarget(args)
	if err != nil {
		return err
	}
	version := resolved.Version

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

	return finishExt(resolved, "enabled", installed, failed)
}

func runExtDisable(_ *cobra.Command, args []string) error {
	exts, resolved, err := resolveExtTarget(args)
	if err != nil {
		return err
	}
	version := resolved.Version

	var disabled, failed []string
	for _, ext := range exts {
		if !php.IsLoaded(resolved.Binary, ext) {
			ui.Info(fmt.Sprintf("%s is not enabled for PHP %s", ext, version))
			continue
		}
		if !disableExt(resolved, ext) {
			failed = append(failed, ext)
			continue
		}
		disabled = append(disabled, ext)
	}

	return finishExt(resolved, "disabled", disabled, failed)
}

func runExtUninstall(_ *cobra.Command, args []string) error {
	exts, resolved, err := resolveExtTarget(args)
	if err != nil {
		return err
	}
	version := resolved.Version

	for _, ext := range exts {
		if php.IsCorePackage(ext) {
			return fmt.Errorf("%s is part of PHP itself, not an extension — use 'phnx php uninstall' to remove PHP", ext)
		}
	}

	ok, err := ui.Confirm(fmt.Sprintf("Uninstall %s from PHP %s?", strings.Join(exts, ", "), version), false)
	if err != nil {
		return err
	}
	if !ok {
		return nil
	}

	var removed, failed []string
	for _, ext := range exts {
		pkgRemoved, err := php.UninstallExtension(resolved, ext)
		if err != nil {
			ui.Error(fmt.Sprintf("uninstalling %s: %v", ext, err))
			failed = append(failed, ext)
			continue
		}
		if !pkgRemoved {
			if !php.IsLoaded(resolved.Binary, ext) {
				ui.Info(fmt.Sprintf("%s is not installed for PHP %s", ext, version))
				continue
			}
			ui.Warn(fmt.Sprintf("%s ships with PHP %s and has no separate package — disabling it instead", ext, version))
		}

		// Comment out leftover directives too: package removal can leave a
		// modified ini behind, which would make FPM warn about a missing module.
		if !disableExt(resolved, ext) {
			failed = append(failed, ext)
			continue
		}
		removed = append(removed, ext)
	}

	return finishExt(resolved, "removed", removed, failed)
}

// disableExt comments out the extension's directives and verifies PHP no
// longer loads it, reporting the outcome. It returns whether that succeeded.
func disableExt(resolved php.ResolvedPHP, ext string) bool {
	version := resolved.Version
	changed, err := php.DisableExtension(resolved, ext)
	if err != nil {
		ui.Error(fmt.Sprintf("disabling %s: %v", ext, err))
		return false
	}
	if php.IsLoaded(resolved.Binary, ext) {
		if !changed {
			ui.Warn(fmt.Sprintf("no extension directive loads %s — it is compiled into PHP %s and cannot be disabled", ext, version))
		} else {
			ui.Warn(fmt.Sprintf("PHP %s still reports %s as loaded — check its .ini", version, ext))
		}
		return false
	}
	ui.Success(fmt.Sprintf("%s disabled for PHP %s", ext, version))
	return true
}
