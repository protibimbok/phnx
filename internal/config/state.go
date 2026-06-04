package config

import "time"

type MySQLConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
}

type Site struct {
	Subdomain string    `json:"subdomain"`
	Path      string    `json:"path"`
	Type      string    `json:"type"`
	PHP       string    `json:"php"`
	Port      int       `json:"port"`
	Internal  bool      `json:"internal"`
	CreatedAt time.Time `json:"created_at"`
}

// PHPVersion records a registered PHP installation.
// Tagged=true means a named version (8.2, 8.4 …) whose socket/service/binary
// are computed on demand by ResolvePHP.
// Tagged=false means an untagged system PHP (e.g. Arch `php` package) whose
// socket/service/binary are stored explicitly at registration time.
type PHPVersion struct {
	Version string `json:"version"`
	Tagged  bool   `json:"tagged"`
	Socket  string `json:"socket"`
	Service string `json:"service"`
	Binary  string `json:"binary"`
}

type Config struct {
	TLD           string      `json:"tld"`
	NginxDir      string      `json:"nginx_dir"`
	NginxSitesDir string      `json:"nginx_sites_dir"`
	DefaultPHP    string      `json:"default_php"`
	RealUser      string      `json:"real_user"`
	RealGroup     string      `json:"real_group"`
	MySQL         MySQLConfig `json:"mysql"`
	Sites         []Site      `json:"sites"`
	PHPVersions   []PHPVersion `json:"php_versions"`
}
