package phpcmd

import (
	"github.com/spf13/cobra"
)

var PHPCmd = &cobra.Command{
	Use:   "php",
	Short: "Manage PHP versions",
}
