package setup

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"

	"github.com/protibimbok/phnx/internal/config"
	"github.com/protibimbok/phnx/internal/ui"
	"github.com/spf13/cobra"
)

var phpmyadminCmd = &cobra.Command{
	Use:   "phpmyadmin",
	Short: "Install phpMyAdmin as an internal site",
	RunE:  runPHPMyAdmin,
}

func init() {
	SetupCmd.AddCommand(phpmyadminCmd)
}

type pmaVersionResp struct {
	Version string `json:"version"`
	Files   []struct {
		Filename string `json:"filename"`
		SHA256   string `json:"sha256"`
	} `json:"files"`
}

func runPHPMyAdmin(_ *cobra.Command, _ []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	installDir := filepath.Join(config.Dir(), "tools", "phpmyadmin")
	tmpDir := filepath.Join(installDir, "tmp")

	if err := os.MkdirAll(tmpDir, 0755); err != nil {
		return fmt.Errorf("creating phpmyadmin dir: %w", err)
	}

	// 1. Fetch latest version info
	ui.Info("Fetching phpMyAdmin version info...")
	verData, err := download("https://www.phpmyadmin.net/home_page/version.json")
	if err != nil {
		return fmt.Errorf("fetching phpMyAdmin version: %w", err)
	}
	var verInfo pmaVersionResp
	if err := json.Unmarshal(verData, &verInfo); err != nil {
		return fmt.Errorf("parsing phpMyAdmin version: %w", err)
	}
	version := verInfo.Version
	ui.Info(fmt.Sprintf("Latest phpMyAdmin version: %s", version))

	// 2. Identify download file
	var zipFilename, expectedSHA string
	for _, f := range verInfo.Files {
		if strings.HasSuffix(f.Filename, ".zip") && !strings.Contains(f.Filename, "source") {
			zipFilename = f.Filename
			expectedSHA = f.SHA256
			break
		}
	}
	if zipFilename == "" {
		zipFilename = fmt.Sprintf("phpMyAdmin-%s-all-languages.zip", version)
	}

	downloadURL := fmt.Sprintf("https://files.phpmyadmin.net/phpMyAdmin/%s/%s", version, zipFilename)
	ui.Info(fmt.Sprintf("Downloading %s...", zipFilename))
	zipData, err := download(downloadURL)
	if err != nil {
		return fmt.Errorf("downloading phpMyAdmin: %w", err)
	}

	// 3. Verify checksum if available
	if expectedSHA != "" {
		h := sha256.New()
		h.Write(zipData)
		actual := hex.EncodeToString(h.Sum(nil))
		if actual != expectedSHA {
			return fmt.Errorf("phpMyAdmin checksum mismatch (got %s, want %s)", actual, expectedSHA)
		}
		ui.Success("Checksum verified")
	}

	// 4. Extract zip
	ui.Info("Extracting phpMyAdmin...")
	if err := extractZip(zipData, installDir); err != nil {
		return fmt.Errorf("extracting phpMyAdmin: %w", err)
	}
	ui.Success(fmt.Sprintf("Extracted to %s", installDir))

	// 5. Write config.inc.php
	blowfishSecret, err := ui.AskText(
		"Blowfish secret (32 chars, blank to generate)",
		"leave blank to generate",
		"",
	)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(blowfishSecret)) < 32 {
		blowfishSecret = randomSecret(32)
		ui.Info(fmt.Sprintf("Generated blowfish secret: %s", blowfishSecret))
	}

	configContent := renderPMAConfig(cfg, blowfishSecret, tmpDir)
	configPath := filepath.Join(installDir, "config.inc.php")
	if err := os.WriteFile(configPath, []byte(configContent), 0644); err != nil {
		return fmt.Errorf("writing config.inc.php: %w", err)
	}
	ui.Success("config.inc.php written")

	// 6. Register as internal site
	if registerInternalSiteFunc == nil {
		return fmt.Errorf("internal: registerInternalSiteFunc not wired up")
	}
	phpVersion := cfg.DefaultPHP
	if phpVersion == "" && len(cfg.PHPVersions) > 0 {
		phpVersion = cfg.PHPVersions[0].Version
	}
	if err := registerInternalSiteFunc("phpmyadmin", installDir, "php", phpVersion, 80); err != nil {
		return fmt.Errorf("registering phpmyadmin site: %w", err)
	}

	ui.Separator()
	ui.Success(fmt.Sprintf("phpMyAdmin installed: http://phpmyadmin.%s", cfg.TLD))
	return nil
}

// registerInternalSiteFunc is set by cmd/root.go to avoid import cycles.
var registerInternalSiteFunc func(subdomain, path, siteType, phpVersion string, port int) error

// SetRegisterFunc wires the init command helper into setup.
func SetRegisterFunc(fn func(subdomain, path, siteType, phpVersion string, port int) error) {
	registerInternalSiteFunc = fn
}

func renderPMAConfig(cfg *config.Config, blowfish, tmpDir string) string {
	return fmt.Sprintf(`<?php
$cfg['blowfish_secret'] = '%s';
$i = 0;
$i++;
$cfg['Servers'][$i]['auth_type'] = 'cookie';
$cfg['Servers'][$i]['host'] = '%s';
$cfg['Servers'][$i]['port'] = %d;
$cfg['Servers'][$i]['connect_type'] = 'tcp';
$cfg['Servers'][$i]['compress'] = false;
$cfg['Servers'][$i]['AllowNoPassword'] = false;
$cfg['UploadDir'] = '';
$cfg['SaveDir'] = '';
$cfg['TempDir'] = '%s';
`,
		blowfish,
		cfg.MySQL.Host,
		cfg.MySQL.Port,
		tmpDir,
	)
}

func extractZip(data []byte, destDir string) error {
	r, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return err
	}

	// Find top-level directory prefix inside the zip
	topDir := ""
	if len(r.File) > 0 {
		parts := strings.SplitN(r.File[0].Name, "/", 2)
		if len(parts) > 1 {
			topDir = parts[0] + "/"
		}
	}

	for _, f := range r.File {
		relPath := strings.TrimPrefix(f.Name, topDir)
		if relPath == "" {
			continue
		}
		destPath := filepath.Join(destDir, relPath)
		// Guard against zip-slip
		if !strings.HasPrefix(filepath.Clean(destPath)+string(os.PathSeparator), filepath.Clean(destDir)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path in zip: %s", f.Name)
		}

		if f.FileInfo().IsDir() {
			os.MkdirAll(destPath, f.Mode())
			continue
		}

		os.MkdirAll(filepath.Dir(destPath), 0755)
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(destPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, f.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, copyErr := io.Copy(out, rc)
		rc.Close()
		out.Close()
		if copyErr != nil {
			return copyErr
		}
	}
	return nil
}

func randomSecret(n int) string {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	for i := range b {
		b[i] = chars[rand.Intn(len(chars))]
	}
	return string(b)
}
