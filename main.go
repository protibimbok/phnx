package main

import (
	"embed"

	"github.com/protibimbok/phnx/cmd"
	"github.com/protibimbok/phnx/internal/fpm"
	"github.com/protibimbok/phnx/internal/nginx"
)

//go:embed templates
var templateFiles embed.FS

func main() {
	nginx.SetFS(templateFiles)
	fpm.SetFS(templateFiles)
	cmd.Execute()
}
