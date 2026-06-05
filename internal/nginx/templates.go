package nginx

import (
	"bytes"
	"fmt"
	"io/fs"
	"text/template"
)

var templateFS fs.FS

// SetFS initializes the embedded template filesystem (called from main).
func SetFS(f fs.FS) {
	sub, err := fs.Sub(f, "templates/nginx")
	if err != nil {
		panic(fmt.Sprintf("nginx template sub-FS: %v", err))
	}
	templateFS = sub
}

type TemplateData struct {
	Port          int
	ServerName    string
	RootDir       string
	SiteName      string
	PHPVersion    string
	FastcgiSocket string
}

func RenderTemplate(siteType string, data TemplateData) (string, error) {
	name, err := templateName(siteType)
	if err != nil {
		return "", err
	}

	content, err := fs.ReadFile(templateFS, name)
	if err != nil {
		return "", fmt.Errorf("reading template %s: %w", name, err)
	}

	tmpl, err := template.New(name).Parse(string(content))
	if err != nil {
		return "", fmt.Errorf("parsing template %s: %w", name, err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("rendering template %s: %w", name, err)
	}
	return buf.String(), nil
}

func templateName(siteType string) (string, error) {
	switch siteType {
	case "laravel":
		return "laravel.conf.tmpl", nil
	case "wordpress":
		return "wordpress.conf.tmpl", nil
	case "php", "vanilla":
		return "vanilla.conf.tmpl", nil
	default:
		return "", fmt.Errorf("unknown site type: %q (valid: laravel, wordpress, php)", siteType)
	}
}
