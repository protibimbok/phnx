package fpm

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/protibimbok/phnx/internal/system"
)

var templateFS fs.FS

// SetFS initializes the embedded template filesystem (called from main).
func SetFS(f fs.FS) {
	sub, err := fs.Sub(f, "templates/fpm")
	if err != nil {
		panic(fmt.Sprintf("fpm template sub-FS: %v", err))
	}
	templateFS = sub
}

type PoolData struct {
	Version    string
	RealUser   string
	RealGroup  string
	WebUser    string
	SocketPath string
}

// WritePool renders and writes the phnx FPM pool config for a PHP version.
func WritePool(version, realUser, realGroup string) error {
	data := PoolData{
		Version:    version,
		RealUser:   realUser,
		RealGroup:  realGroup,
		WebUser:    system.NginxWebUserFor(realUser),
		SocketPath: system.Platform.PhpFpmSock(version),
	}

	content, err := fs.ReadFile(templateFS, "pool.conf.tmpl")
	if err != nil {
		return fmt.Errorf("reading FPM pool template: %w", err)
	}

	tmpl, err := template.New("pool").Parse(string(content))
	if err != nil {
		return fmt.Errorf("parsing FPM pool template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return fmt.Errorf("rendering FPM pool template: %w", err)
	}

	poolDir := system.Platform.PhpFpmPoolDir(version)
	if err := system.MkdirAll(poolDir); err != nil {
		return err
	}

	poolPath := filepath.Join(poolDir, "phnx.conf")
	return system.WriteFile(poolPath, buf.String())
}

// RemovePool deletes the phnx pool config for a PHP version.
func RemovePool(version string) error {
	poolPath := filepath.Join(system.Platform.PhpFpmPoolDir(version), "phnx.conf")
	return system.RemoveFile(poolPath)
}

// systemPoolDirs lists where distro-managed php-fpm pools live. The untagged
// (system) php-fpm uses these rather than a phnx-written pool.
var systemPoolDirs = []string{
	"/etc/php/php-fpm.d", // Arch
	"/etc/php-fpm.d",     // Fedora/RHEL
}

// EnsurePoolRunsAsUser makes the distro php-fpm pool that listens on socketPath
// run as the given user/group. This is needed for untagged/system php-fpm
// installs (which use the distro pool, e.g. www.conf, running as http/www-data):
// when site roots live under user-owned, non-world-traversable paths the worker
// must run as the real user to read them. Edits the pool file in place and
// returns its path. The caller is responsible for restarting the FPM service.
func EnsurePoolRunsAsUser(socketPath, user, group string) (string, error) {
	poolFile, err := findPoolFileBySocket(socketPath)
	if err != nil {
		return "", err
	}

	// Already running as the desired user?
	checkCmd := fmt.Sprintf(
		`grep -qE '^[[:space:]]*user[[:space:]]*=[[:space:]]*%s[[:space:]]*$' %s`,
		escapeERE(user), poolFile,
	)
	if _, err := system.OutputUser("bash", "-c", checkCmd); err == nil {
		return poolFile, nil
	}

	// Replace the active (uncommented) user/group directives. Commented sample
	// lines start with ';' and are left untouched. listen.owner/listen.group are
	// not matched because they don't start with "user"/"group".
	script := fmt.Sprintf(
		`sed -i -E 's/^([[:space:]]*)user[[:space:]]*=.*/\1user = %s/; s/^([[:space:]]*)group[[:space:]]*=.*/\1group = %s/' %s`,
		user, group, poolFile,
	)
	if err := system.Run("bash", "-c", script); err != nil {
		return "", fmt.Errorf("updating pool user in %s: %w", poolFile, err)
	}
	return poolFile, nil
}

// findPoolFileBySocket locates the distro pool .conf whose `listen` directive
// matches socketPath, falling back to www.conf in the standard locations.
func findPoolFileBySocket(socketPath string) (string, error) {
	dirs := strings.Join(systemPoolDirs, " ")
	findCmd := fmt.Sprintf(
		`grep -rlE '^[[:space:]]*listen[[:space:]]*=[[:space:]]*%s[[:space:]]*$' %s 2>/dev/null | head -n1`,
		escapeERE(socketPath), dirs,
	)
	if out, err := system.OutputUser("bash", "-c", findCmd); err == nil {
		if f := strings.TrimSpace(out); f != "" {
			return f, nil
		}
	}
	for _, dir := range systemPoolDirs {
		candidate := filepath.Join(dir, "www.conf")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
	}
	return "", fmt.Errorf("could not locate php-fpm pool for socket %s", socketPath)
}

// escapeERE escapes characters that are special in POSIX extended regular
// expressions so a literal string (e.g. a socket path) can be matched safely.
func escapeERE(s string) string {
	specials := `\.[]{}()*+?|^$/`
	var b strings.Builder
	for _, r := range s {
		if strings.ContainsRune(specials, r) {
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// EnsureRunning makes sure the named FPM service is running, starting it if needed.
// Pass resolved.Service from php.ResolvePHP rather than a raw version string.
func EnsureRunning(serviceName string) error {
	svc := system.NewServiceManager()
	running, err := svc.IsRunning(serviceName)
	if err != nil {
		return fmt.Errorf("checking FPM status for %s: %w", serviceName, err)
	}
	if !running {
		fmt.Printf("Starting %s...\n", serviceName)
		if err := svc.Start(serviceName); err != nil {
			return fmt.Errorf("starting %s: %w", serviceName, err)
		}
	}
	return nil
}
