package setup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/php"
	"github.com/protibimbok/phnx/internal/site"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

const ppaReleaseURL = "https://api.github.com/repos/protibimbok/phpPgAdmin/releases/latest"

// ppaMinPHP is the oldest PHP version phpPgAdmin runs on.
const ppaMinPHP = "8.3"

var phppgadminCmd = &cobra.Command{
	Use:   "phppgadmin",
	Short: "Install phpPgAdmin as an internal site",
	RunE:  runPHPPgAdmin,
}

func init() {
	SetupCmd.AddCommand(phppgadminCmd)
}

type ppaReleaseResp struct {
	TagName string `json:"tag_name"`
	Assets  []struct {
		Name               string `json:"name"`
		BrowserDownloadURL string `json:"browser_download_url"`
		Digest             string `json:"digest"`
	} `json:"assets"`
}

func runPHPPgAdmin(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if err := ensurePostgresServer(cfg); err != nil {
		return err
	}
	// The debugger is optional; phpPgAdmin hides Debug without it.
	if err := ensurePostgresDebugger(true); err != nil {
		ui.Warn(fmt.Sprintf("PL/pgSQL debugger not set up: %v", err))
		ui.Warn("phpPgAdmin works without it; run `phnx setup pgdebugger` to retry.")
	}

	installDir := filepath.Join(config.Dir(), "tools", "phppgadmin")
	if err := os.MkdirAll(installDir, 0755); err != nil {
		return fmt.Errorf("creating phppgadmin dir: %w", err)
	}

	ui.Info("Fetching phpPgAdmin release info...")
	relData, err := download(ppaReleaseURL)
	if err != nil {
		return fmt.Errorf("fetching phpPgAdmin release: %w", err)
	}
	var rel ppaReleaseResp
	if err := json.Unmarshal(relData, &rel); err != nil {
		return fmt.Errorf("parsing phpPgAdmin release: %w", err)
	}
	ui.Info(fmt.Sprintf("Latest phpPgAdmin version: %s", strings.TrimPrefix(rel.TagName, "v")))

	var zipName, downloadURL, expectedSHA string
	for _, a := range rel.Assets {
		if strings.HasSuffix(a.Name, ".zip") {
			zipName = a.Name
			downloadURL = a.BrowserDownloadURL
			expectedSHA = strings.TrimPrefix(a.Digest, "sha256:")
			break
		}
	}
	if downloadURL == "" {
		return fmt.Errorf("no zip asset found in phpPgAdmin release %s", rel.TagName)
	}

	ui.Info(fmt.Sprintf("Downloading %s...", zipName))
	zipData, err := download(downloadURL)
	if err != nil {
		return fmt.Errorf("downloading phpPgAdmin: %w", err)
	}

	if expectedSHA != "" {
		h := sha256.New()
		h.Write(zipData)
		actual := hex.EncodeToString(h.Sum(nil))
		if actual != expectedSHA {
			return fmt.Errorf("phpPgAdmin checksum mismatch (got %s, want %s)", actual, expectedSHA)
		}
		ui.Success("Checksum verified")
	}

	ui.Info("Extracting phpPgAdmin...")
	if err := extractZip(zipData, installDir); err != nil {
		return fmt.Errorf("extracting phpPgAdmin: %w", err)
	}
	ui.Success(fmt.Sprintf("Extracted to %s", installDir))

	dbConfigPath := filepath.Join(installDir, "config", "db.php")
	if err := os.WriteFile(dbConfigPath, []byte(renderPPADBConfig(cfg)), 0644); err != nil {
		return fmt.Errorf("writing config/db.php: %w", err)
	}
	ui.Success("config/db.php written")

	// Register as internal site. phpPgAdmin serves from public/ through a
	// front controller, which is what the laravel template does.
	phpVersion := pickPPAPHPVersion(cfg)
	warnPPAPHPRequirements(cfg, phpVersion)
	if err := site.RegisterInternal("phppgadmin", installDir, "laravel", phpVersion, 80); err != nil {
		return fmt.Errorf("registering phppgadmin site: %w", err)
	}

	ui.Separator()
	ui.Success(fmt.Sprintf("phpPgAdmin installed: http://phppgadmin.%s", cfg.TLD))
	return nil
}

func renderPPADBConfig(cfg *config.Config) string {
	host := cfg.Postgres.Host
	if host == "" {
		host = "127.0.0.1"
	}
	port := cfg.Postgres.Port
	if port == 0 {
		port = 5432
	}
	return fmt.Sprintf(`<?php

declare(strict_types=1);

// Written by phnx. Each value can still be overridden from the environment.
return [
    'host' => getenv('PGADMIN_DB_HOST') ?: %s,
    'port' => (int) (getenv('PGADMIN_DB_PORT') ?: %d),
    'sslmode' => getenv('PGADMIN_DB_SSLMODE') ?: 'prefer',
    'database' => getenv('PGADMIN_DB_DATABASE') ?: null,
    'timeout' => (int) (getenv('PGADMIN_DB_TIMEOUT') ?: 10),
];
`, phpQuote(host), port)
}

// phpQuote returns s as a single-quoted PHP string literal.
func phpQuote(s string) string {
	return "'" + strings.NewReplacer(`\`, `\\`, `'`, `\'`).Replace(s) + "'"
}

// pickPPAPHPVersion prefers the default PHP version, falling back to the
// newest registered one when the default is older than phpPgAdmin supports.
func pickPPAPHPVersion(cfg *config.Config) string {
	best := cfg.DefaultPHP
	if best == "" && len(cfg.PHPVersions) > 0 {
		best = cfg.PHPVersions[0].Version
	}
	if phpAtLeast(best, ppaMinPHP) {
		return best
	}
	for _, v := range cfg.PHPVersions {
		if phpAtLeast(v.Version, ppaMinPHP) && (!phpAtLeast(best, ppaMinPHP) || phpAtLeast(v.Version, best)) {
			best = v.Version
		}
	}
	return best
}

// phpAtLeast reports whether version v (e.g. "8.3") is >= minVer.
func phpAtLeast(v, minVer string) bool {
	parse := func(s string) (int, int, bool) {
		parts := strings.SplitN(s, ".", 3)
		if len(parts) < 2 {
			return 0, 0, false
		}
		major, err1 := strconv.Atoi(parts[0])
		minor, err2 := strconv.Atoi(parts[1])
		return major, minor, err1 == nil && err2 == nil
	}
	vMaj, vMin, ok := parse(v)
	if !ok {
		return false
	}
	mMaj, mMin, _ := parse(minVer)
	return vMaj > mMaj || (vMaj == mMaj && vMin >= mMin)
}

// warnPPAPHPRequirements hints at what phpPgAdmin needs from PHP: version 8.3+
// with the pgsql extension (server connections) and pdo_sqlite (its own storage).
func warnPPAPHPRequirements(cfg *config.Config, version string) {
	if version == "" {
		return
	}
	if !phpAtLeast(version, ppaMinPHP) {
		ui.Warn(fmt.Sprintf("phpPgAdmin needs PHP %s or newer, but only PHP %s is available.", ppaMinPHP, version))
		ui.Warn(fmt.Sprintf("Run `phnx php install %s`, then re-run `phnx setup phppgadmin`.", ppaMinPHP))
		return
	}
	resolved, err := php.ResolvePHP(cfg, version)
	if err != nil {
		return
	}
	// extension module → package name `phnx php ext install` takes
	for ext, pkg := range map[string]string{"pgsql": "pgsql", "pdo_sqlite": "sqlite3"} {
		if !php.IsLoaded(resolved.Binary, ext) {
			ui.Warn(fmt.Sprintf("PHP %s does not have the %s extension loaded.", version, ext))
			ui.Warn(fmt.Sprintf("Run `phnx php ext install %s` before using phpPgAdmin.", pkg))
		}
	}
}
