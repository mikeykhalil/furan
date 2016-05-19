package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

var cliBuildRequest buildRequest
var tags string
var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build and push a docker image from repo",
	Long: `Build a Docker image from the specified git repository and push
to the specified image repository.`,
	Run: build,
}

func init() {
	buildCmd.PersistentFlags().StringVarP(&cliBuildRequest.SourceRepo, "source-repo", "s", "", "source git repo")
	buildCmd.PersistentFlags().StringVarP(&cliBuildRequest.SourceBranch, "source-branch", "b", "master", "source git branch")
	buildCmd.PersistentFlags().StringVarP(&cliBuildRequest.ImageRepo, "image-repo", "i", "", "push to image repo")
	buildCmd.PersistentFlags().StringVarP(&tags, "tags", "t", "master", "image tags (comma-delimited)")
	buildCmd.PersistentFlags().BoolVarP(&cliBuildRequest.TagWithSHA, "tag-sha", "t", false, "additionally tag with git commit SHA")
	buildCmd.PersistentFlags().BoolVarP(&cliBuildRequest.PullSquashed, "pull-squashed", "q", false, "after pushing, pull squashed image to prime cache (only supported by quay.io image repositories)")
	RootCmd.AddCommand(buildCmd)
}

func build(cmd *cobra.Command, args []string) {
	cliBuildRequest.Tags = strings.Split(tags, ",")
	builder, err := NewImageBuilder()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating builder: %v\n", err)
		os.Exit(1)
	}
	err = builder.Build(&cliBuildRequest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error building: %v\n", err)
		os.Exit(1)
	}
}
