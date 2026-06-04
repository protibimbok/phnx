package nginx

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/protibimbok/phnx/internal/system"
)

// WriteSiteConfig renders and writes an nginx site config (via sudo tee).
func WriteSiteConfig(sitesDir, subdomain, siteType string, data TemplateData) error {
	content, err := RenderTemplate(siteType, data)
	if err != nil {
		return err
	}
	return system.WriteFile(SiteConfigPath(sitesDir, subdomain), content)
}

// RemoveSiteConfig deletes the nginx config file for the given subdomain.
func RemoveSiteConfig(sitesDir, subdomain string) error {
	return system.RemoveFile(SiteConfigPath(sitesDir, subdomain))
}

// SiteConfigPath returns the path to the nginx config for a subdomain.
func SiteConfigPath(sitesDir, subdomain string) string {
	return filepath.Join(sitesDir, subdomain+".conf")
}

// EnsureSitesDirIncluded adds the include directive to nginx.conf if missing.
func EnsureSitesDirIncluded(nginxDir, sitesDir string) error {
	nginxConf := filepath.Join(nginxDir, "nginx.conf")
	includeDirective := fmt.Sprintf("include %s/*.conf;", sitesDir)

	alreadyPresent := func() bool {
		checkCmd := fmt.Sprintf("grep -qF %q %s", includeDirective, nginxConf)
		_, err := system.OutputUser("bash", "-c", checkCmd)
		return err == nil
	}

	if alreadyPresent() {
		return nil
	}

	// Try sed: insert after the last existing include line inside the http block.
	// sed exits 0 even when the pattern doesn't match, so we verify afterwards.
	var sedFlag string
	if runtime.GOOS == "darwin" {
		sedFlag = `''`
	}
	escapedDir := strings.ReplaceAll(sitesDir, "/", `\/`)
	script := fmt.Sprintf(
		`sed -i %s '/^[[:space:]]*include.*sites-enabled/a\\    include %s\/*.conf;' %s`,
		sedFlag, escapedDir, nginxConf,
	)
	_ = system.Run("bash", "-c", script) // ignore exit code — sed returns 0 on no-match too

	if alreadyPresent() {
		return nil
	}

	// sed anchor not found (no sites-enabled line) — fall back to inserting
	// before the closing brace of the http block.
	return appendIncludeToHTTPBlock(nginxConf, includeDirective)
}

// EnsureWorkerUser sets the nginx worker user/group in nginx.conf so that workers
// run as the real user and can access project directories (important on WSL and
// setups where site roots aren't world-readable by the default nginx system user).
func EnsureWorkerUser(nginxDir, user, group string) error {
	nginxConf := filepath.Join(nginxDir, "nginx.conf")
	directive := fmt.Sprintf("user %s %s;", user, group)

	// Check if the correct user is already set.
	checkCmd := fmt.Sprintf("grep -qE '^[[:space:]]*user[[:space:]]+%s([[:space:]]|;)' %s", user, nginxConf)
	if _, err := system.OutputUser("bash", "-c", checkCmd); err == nil {
		return nil
	}

	// Replace any existing user directive (commented or not) with the correct one.
	escapedDirective := strings.ReplaceAll(directive, "/", `\/`)
	script := fmt.Sprintf(
		`sed -i 's/^[[:space:]]*#*[[:space:]]*user[[:space:]][^;]*;/%s/' %s`,
		escapedDirective, nginxConf,
	)
	if err := system.Run("bash", "-c", script); err != nil {
		return fmt.Errorf("updating nginx worker user: %w", err)
	}

	// Verify it took effect; if the file had no user line at all, prepend one.
	if _, err := system.OutputUser("bash", "-c", checkCmd); err != nil {
		prependScript := fmt.Sprintf(
			`sed -i '1s/^/%s\n/' %s`,
			escapedDirective, nginxConf,
		)
		return system.Run("bash", "-c", prependScript)
	}
	return nil
}

// appendIncludeToHTTPBlock inserts the include line before the last closing brace
// of the http { } block in nginx.conf using a Python one-liner (available on both
// macOS and most Linux distros).
func appendIncludeToHTTPBlock(nginxConf, includeDirective string) error {
	script := fmt.Sprintf(
		`python3 -c "
import re, sys
txt = open('%s').read()
directive = '    %s\n'
if directive.strip() in txt:
    sys.exit(0)
# insert before last closing brace
idx = txt.rfind('}')
if idx == -1:
    sys.exit(1)
txt = txt[:idx] + directive + txt[idx:]
open('%s', 'w').write(txt)
"`,
		nginxConf, includeDirective, nginxConf,
	)
	return system.Run("bash", "-c", script)
}
