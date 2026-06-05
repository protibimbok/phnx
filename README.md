# phnx

PHP + Nginx local development environment manager. `phnx` provisions nginx sites, PHP-FPM pools, `/etc/hosts` entries, and helper tools (Composer, WP-CLI, phpMyAdmin) so you can serve a Laravel, WordPress, or plain PHP project at `http://<name>.test` with a single command.

Works on Linux (Arch, Debian/Ubuntu, Fedora/RHEL) and macOS (Homebrew).

## Installation

### Homebrew (macOS / Linux)

```bash
brew tap protibimbok/pkg-dist
brew install phnx
```

### apt (Debian / Ubuntu)

```bash
# One-time: install the signing key
curl -fsSL \
  https://github.com/protibimbok/pkg-dist/raw/master/public.gpg \
  | sudo gpg --dearmor \
  -o /usr/share/keyrings/protibimbok.gpg

# One-time: add the repository
echo "deb [signed-by=/usr/share/keyrings/protibimbok.gpg] \
  https://protibimbok.github.io/pkg-dist/apt stable main" \
  | sudo tee /etc/apt/sources.list.d/protibimbok.list

sudo apt update
sudo apt install phnx
```

### pacman / AUR (Arch Linux)

```bash
yay -S phnx-bin
# or
paru -S phnx-bin
```

### rpm (Fedora / RHEL / openSUSE)

Download the `.rpm` from the [latest release](https://github.com/protibimbok/phnx/releases/latest):

```bash
sudo rpm -i phnx_linux_amd64.rpm
```

### Alpine Linux

```bash
# Download the .apk from the latest release, then:
sudo apk add --allow-untrusted phnx_linux_amd64.apk
```

### Shell installer (Linux / macOS)

```bash
curl -fsSL https://raw.githubusercontent.com/protibimbok/phnx/main/scripts/install.sh | bash
```

Installs to `/usr/local/bin` by default. Override with `INSTALL_DIR=/your/path`.

### go install

```bash
go install github.com/protibimbok/phnx@latest
```

### Prerequisites

`phnx` drives existing system services rather than bundling them. You need:

- **nginx** — installed and present at `/etc/nginx` (Linux) or the Homebrew prefix (macOS).
- **PHP-FPM** — either an existing install or let `phnx configure` install one for you.
- **MySQL/MariaDB** — only required for WordPress scaffolding and phpMyAdmin.

`phnx` escalates to `sudo` automatically for privileged steps (editing `/etc/nginx`, `/etc/hosts`, installing packages, managing services), so run it as your regular user — not as root.

---

## Quick start

```bash
# 1. One-time setup: detects nginx + PHP, wires up the phnx sites directory,
#    sets the worker user, and saves config to ~/.phnx/config.json
phnx configure

# 2. From your project directory, register a site (subdomain defaults to the
#    folder name). Prompts for the site type if --type is omitted.
cd ~/code/myapp
phnx init --type laravel

# 3. Open it
#    → http://myapp.test
```

---

## Commands

### `phnx configure`

First-time, idempotent setup. Detects nginx and installed PHP versions, prompts for a TLD (default `test`) and a default PHP version, creates the `phnx-sites` include directory, sets the nginx worker user and PHP-FPM pool user so workers can read your project files, and writes `~/.phnx/config.json`.

```bash
phnx configure
```

Re-run it any time — it only changes what's missing.

---

### `phnx init [subdomain]`

Registers the current directory as a local site: writes the nginx config, adds an `/etc/hosts` entry, ensures the right PHP-FPM pool is running, and reloads nginx (rolling back on failure).

```bash
phnx init                       # subdomain = current folder name, prompts for type
phnx init myapp                 # explicit subdomain
phnx init --type wordpress      # laravel | wordpress | php
phnx init --php 8.2             # use a specific PHP version
phnx init --port 8080           # listen on a non-default port
```

- **Laravel** — if the directory is empty, offers to run `composer create-project laravel/laravel`.
- **WordPress** — if the directory is empty, downloads WordPress, creates a database, and (if WP-CLI is present) generates `wp-config.php`.
- PHP version resolution order: `--php` flag → `.php-version` file → config default.

---

### `phnx list`

Lists registered sites with a health check (nginx config present, FPM socket up, project path exists).

```bash
phnx list
phnx list --all   # also show internal phnx-managed sites (e.g. phpmyadmin)
```

---

### `phnx remove [subdomain]`

Removes a site's nginx config, `/etc/hosts` entry, and config record, then reloads nginx. Optionally deletes log files and the project directory.

```bash
phnx remove myapp   # by subdomain
phnx remove         # matches the site for the current directory
```

---

### `phnx php`

Manage PHP versions. Installs come from the ondrej PPA (Debian/Ubuntu), versioned AUR packages (Arch), or the shivammathur tap (macOS).

```bash
phnx php install 8.3     # install a version + write its FPM pool + start the service
phnx php list            # show versions, install/FPM status, site counts, default
phnx php default 8.3     # set the default version (updates /usr/local/bin/php)
phnx php pin 8.2         # write .php-version for the current project + re-point its site
phnx php uninstall 8.3   # remove a version (blocked while sites still use it)
```

---

### `phnx setup`

Install optional helper tools. Downloads are checksum-verified.

```bash
phnx setup composer      # install Composer to /usr/local/bin/composer
phnx setup wpcli         # install WP-CLI to /usr/local/bin/wp
phnx setup phpmyadmin    # install phpMyAdmin as an internal site → http://phpmyadmin.<tld>
```

---

## How it works

```
phnx init  (in ~/code/myapp)
   │
   ├─► writes /etc/nginx/phnx-sites/myapp.conf   (rendered from a site template)
   ├─► adds  "127.0.0.1 myapp.test"  to /etc/hosts
   ├─► ensures the PHP-FPM pool/socket for the chosen version is running
   └─► nginx -t && nginx -s reload
                         │
        request to http://myapp.test
                         │
        nginx (worker runs as you) ──fastcgi──► PHP-FPM (pool runs as you)
```

- **Sites** are individual nginx config files in `/etc/nginx/phnx-sites/`, pulled in by a single `include` directive added to `nginx.conf`. Templates exist for `laravel`, `wordpress`, and `php`.
- **PHP versions** are either *tagged* (named installs like `8.2`/`8.4`, whose socket/service/binary `phnx` computes) or *untagged* (the distro's system PHP-FPM, whose paths are stored at registration). `phnx` makes both nginx and PHP-FPM run as your user so they can read project roots that live under user-owned paths.
- **Per-project PHP** is controlled by a `.php-version` file (`phnx php pin`), so different sites can run on different versions simultaneously.

---

## Configuration

State lives in `~/.phnx/config.json` (created by `phnx configure`):

| Key | Description |
|-----|-------------|
| `tld` | Domain suffix for sites (default `test`) |
| `nginx_dir` / `nginx_sites_dir` | nginx config root and the phnx include directory |
| `default_php` | Default PHP version for new sites |
| `real_user` / `real_group` | The user/group nginx + PHP-FPM workers run as |
| `mysql` | Host/port/user/password used for WordPress + phpMyAdmin |
| `sites` | Registered sites |
| `php_versions` | Registered PHP installations |

Other relevant locations:

- nginx site configs: `/etc/nginx/phnx-sites/*.conf`
- internal tools (e.g. phpMyAdmin): `~/.phnx/tools/`
- per-project version pin: `.php-version` in the project root

---

## Building from source

```bash
make build        # builds ./phnx with version info
make install      # go install into $GOBIN
make snapshot     # local goreleaser snapshot build (dist/)
make lint         # go vet ./...
```

Requires Go (see `go.mod` for the minimum version).

---

## Release setup (for maintainers)

Homebrew and apt distribution are managed centrally in [protibimbok/pkg-dist](https://github.com/protibimbok/pkg-dist). This repo only builds binaries and publishes GitHub Releases.

### Required GitHub secrets (this repo)

| Secret | Purpose | Required |
|--------|---------|----------|
| `GITHUB_TOKEN` | Create GitHub Releases | Auto-provided |
| `PKG_DIST_TOKEN` | Trigger pkg-dist update after release | For Homebrew + apt |
| `AUR_KEY` | SSH private key for AUR updates | For AUR |

### Creating a release

```bash
git tag v1.0.0
git push origin v1.0.0
```

The release workflow builds binaries, publishes to GitHub Releases, updates AUR, and notifies pkg-dist to update Homebrew casks and the apt repository.

See [pkg-dist](https://github.com/protibimbok/pkg-dist) for signing keys, apt repo setup, and GitHub Pages configuration.

---

## License

MIT
