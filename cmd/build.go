package cmd

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/dollarshaveclub/go-lib/httpreq"
	"github.com/spf13/cobra"
)

var cliBuildRequest buildRequest
var tags string
var remoteURL string

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
	buildCmd.PersistentFlags().BoolVarP(&cliBuildRequest.TagWithSHA, "tag-sha", "x", false, "additionally tag with git commit SHA")
	buildCmd.PersistentFlags().BoolVarP(&cliBuildRequest.PullSquashed, "pull-squashed", "q", false, "after pushing, pull squashed image to prime cache (only supported by quay.io image repositories)")
	buildCmd.PersistentFlags().StringVarP(&remoteURL, "remote-url", "r", "", "Remote URL of Furan server (otherwise build locally)")
	RootCmd.AddCommand(buildCmd)
}

func build(cmd *cobra.Command, args []string) {
	cliBuildRequest.Tags = strings.Split(tags, ",")
	if remoteURL == "" {
		gitConfig.pubKeyLocalPath, gitConfig.privKeyLocalPath = writeSSHKeypair()
		defer rmTempFiles(gitConfig.pubKeyLocalPath, gitConfig.privKeyLocalPath)

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
	} else {
		remoteBuild()
	}
}

func remoteBuild() {
	j, err := json.Marshal(&cliBuildRequest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling request: %v\n", err)
		os.Exit(1)
	}
	url := fmt.Sprintf("%v/build", remoteURL)
	headers := map[string]string{"Content-Type": "application/json"}
	r, err := httpreq.HTTPRequest(url, "POST", bytes.NewBuffer(j), headers, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error triggering remote build: %v\n", err)
		os.Exit(1)
	}
	resp := requestResponse{}
	err = json.Unmarshal(r.BodyBytes, &resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error unmarshaling response: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("build_id: %v\n", resp.BuildID)
}
