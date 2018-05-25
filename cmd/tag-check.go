package cmd

import (
	"github.com/spf13/cobra"
)

// tagCheckCmd represents the tagCheck command
var tagCheckCmd = &cobra.Command{
	Use:   "tag-check",
	Short: "Checks for existing tag on s3 server",
	Run:   tagCheck,
}

func init() {
}

func tagCheck(cmd *cobra.Command, args []string) {
}
