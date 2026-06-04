package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// UnmarshalJSON migrates old config.json files that stored php_versions as
// []string (e.g. ["8.2"]) into the new []PHPVersion format on first load.
func (c *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	type rawConfig struct {
		Alias
		RawPHPVersions json.RawMessage `json:"php_versions"`
	}
	var raw rawConfig
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	*c = Config(raw.Alias)

	if len(raw.RawPHPVersions) == 0 || string(raw.RawPHPVersions) == "null" {
		return nil
	}
	var entries []PHPVersion
	if err := json.Unmarshal(raw.RawPHPVersions, &entries); err == nil {
		c.PHPVersions = entries
		return nil
	}
	// Legacy format: []string — promote each to a tagged PHPVersion.
	var versions []string
	if err := json.Unmarshal(raw.RawPHPVersions, &versions); err != nil {
		return fmt.Errorf("parsing php_versions: %w", err)
	}
	for _, v := range versions {
		c.PHPVersions = append(c.PHPVersions, PHPVersion{Version: v, Tagged: true})
	}
	return nil
}

const (
	configDir  = ".phnx"
	configFile = "config.json"
	lockFile   = "config.lock"
)

var mu sync.Mutex

func Dir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, configDir)
}

func Path() string {
	return filepath.Join(Dir(), configFile)
}

func lockPath() string {
	return filepath.Join(Dir(), lockFile)
}

func Load() (*Config, error) {
	data, err := os.ReadFile(Path())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("phnx is not configured — run 'phnx configure' first")
		}
		return nil, fmt.Errorf("reading config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config: %w", err)
	}
	return &cfg, nil
}

func Save(cfg *Config) error {
	mu.Lock()
	defer mu.Unlock()

	if err := os.MkdirAll(Dir(), 0755); err != nil {
		return fmt.Errorf("creating config dir: %w", err)
	}

	lf, err := acquireLock()
	if err != nil {
		return err
	}
	defer releaseLock(lf)

	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling config: %w", err)
	}

	tmp := Path() + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return fmt.Errorf("writing config: %w", err)
	}
	if err := os.Rename(tmp, Path()); err != nil {
		return fmt.Errorf("saving config: %w", err)
	}
	return nil
}

func acquireLock() (*os.File, error) {
	path := lockPath()
	deadline := time.Now().Add(10 * time.Second)
	for {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			return f, nil
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timeout waiting for config lock")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func releaseLock(f *os.File) {
	f.Close()
	os.Remove(lockPath())
}

func (c *Config) FindSite(subdomain string) *Site {
	for i := range c.Sites {
		if c.Sites[i].Subdomain == subdomain {
			return &c.Sites[i]
		}
	}
	return nil
}

func (c *Config) FindSiteByPath(path string) *Site {
	for i := range c.Sites {
		if c.Sites[i].Path == path {
			return &c.Sites[i]
		}
	}
	return nil
}

func (c *Config) AddSite(site Site) {
	c.Sites = append(c.Sites, site)
}

func (c *Config) RemoveSite(subdomain string) bool {
	for i, s := range c.Sites {
		if s.Subdomain == subdomain {
			c.Sites = append(c.Sites[:i], c.Sites[i+1:]...)
			return true
		}
	}
	return false
}

func (c *Config) FindPHPVersion(version string) *PHPVersion {
	for i := range c.PHPVersions {
		if c.PHPVersions[i].Version == version {
			return &c.PHPVersions[i]
		}
	}
	return nil
}

func (c *Config) HasPHPVersion(version string) bool {
	return c.FindPHPVersion(version) != nil
}

func (c *Config) AddPHPVersion(v PHPVersion) {
	if !c.HasPHPVersion(v.Version) {
		c.PHPVersions = append(c.PHPVersions, v)
	}
}

func (c *Config) RemovePHPVersion(version string) {
	for i, v := range c.PHPVersions {
		if v.Version == version {
			c.PHPVersions = append(c.PHPVersions[:i], c.PHPVersions[i+1:]...)
			return
		}
	}
}

func (c *Config) SitesDomain(subdomain string) string {
	return subdomain + "." + c.TLD
}

func (c *Config) SiteURL(subdomain string) string {
	s := c.FindSite(subdomain)
	if s == nil {
		return ""
	}
	if s.Port == 443 {
		return "https://" + c.SitesDomain(subdomain)
	}
	if s.Port == 80 {
		return "http://" + c.SitesDomain(subdomain)
	}
	return fmt.Sprintf("http://%s:%d", c.SitesDomain(subdomain), s.Port)
}
