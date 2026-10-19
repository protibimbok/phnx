package database

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// PsqlBinary returns the path to the psql client. Homebrew's versioned
// postgresql@N formulae are keg-only, so on macOS it also looks inside their
// opt prefixes, preferring the newest version.
func PsqlBinary() (string, error) {
	if p, err := exec.LookPath("psql"); err == nil {
		return p, nil
	}
	if runtime.GOOS == "darwin" {
		var found []string
		for _, prefix := range []string{"/opt/homebrew/opt", "/usr/local/opt"} {
			matches, _ := filepath.Glob(filepath.Join(prefix, "postgresql@*", "bin", "psql"))
			found = append(found, matches...)
		}
		if len(found) > 0 {
			sort.Strings(found)
			return found[len(found)-1], nil
		}
	}
	return "", fmt.Errorf("psql client not found in PATH")
}

// CreatePostgres creates the named database if it does not exist yet,
// connecting over TCP with password authentication.
func CreatePostgres(host string, port int, user, password, name string) error {
	exists, err := runPsql(host, port, user, password,
		fmt.Sprintf("SELECT 1 FROM pg_database WHERE datname = %s;", QuotePgLiteral(name)))
	if err != nil {
		return err
	}
	if strings.TrimSpace(exists) == "1" {
		return nil
	}
	_, err = runPsql(host, port, user, password,
		fmt.Sprintf("CREATE DATABASE %s;", QuotePgIdent(name)))
	return err
}

func runPsql(host string, port int, user, password, sql string) (string, error) {
	psql, err := PsqlBinary()
	if err != nil {
		return "", err
	}
	// -w never prompts for a password; it comes from PGPASSWORD instead.
	cmd := exec.Command(psql,
		"-h", host,
		"-p", fmt.Sprintf("%d", port),
		"-U", user,
		"-d", "postgres",
		"-w", "-tAX",
		"-v", "ON_ERROR_STOP=1",
		"-c", sql,
	)
	cmd.Env = append(os.Environ(), "PGPASSWORD="+password)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w\n%s", err, strings.TrimSpace(string(out)))
	}
	return string(out), nil
}

// QuotePgIdent quotes s as a PostgreSQL identifier.
func QuotePgIdent(s string) string {
	return `"` + strings.ReplaceAll(s, `"`, `""`) + `"`
}

// QuotePgLiteral quotes s as a PostgreSQL string literal.
func QuotePgLiteral(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
