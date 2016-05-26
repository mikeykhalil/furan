package cmd

import (
	"bytes"
	"fmt"
	"os"
	"strings"

	"golang.org/x/net/context"

	"github.com/dollarshaveclub/go-lib/httpreq"
	"github.com/golang/protobuf/jsonpb"
	"github.com/spf13/cobra"
)

var cliBuildRequest = BuildRequest{
	Build: &BuildDefinition{
		Ref: &GitRefDefinition{},
	},
	Push: &PushDefinition{
		Registry: &PushRegistryDefinition{},
		S3:       &PushS3Definition{},
	},
}
var tags string
var remoteURL string
var ghtoken string

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build and push a docker image from repo",
	Long: `Build a Docker image from the specified git repository and push
to the specified image repository.`,
	Run: build,
}

func init() {
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.GithubRepo, "github-repo", "", "source github repo")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.Ref.Branch, "source-branch", "master", "source git branch")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.Ref.Sha, "source-sha", "", "source git commit SHA")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.Registry.Repo, "image-repo", "", "push to image repo")
	buildCmd.PersistentFlags().StringVar(&tags, "tags", "master", "image tags (comma-delimited)")
	buildCmd.PersistentFlags().BoolVar(&cliBuildRequest.Build.TagWithCommitSha, "tag-sha", false, "additionally tag with git commit SHA")
	buildCmd.PersistentFlags().StringVar(&remoteURL, "remote-url", "", "Remote URL of Furan server (otherwise build locally)")
	RootCmd.AddCommand(buildCmd)
}

func build(cmd *cobra.Command, args []string) {
	cliBuildRequest.Build.Tags = strings.Split(tags, ",")
	if remoteURL == "" {
		builder, err := NewImageBuilder(gitConfig.token)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error creating builder: %v\n", err)
			os.Exit(1)
		}
		err = builder.Build(context.TODO(), cliBuildRequest.Build)
		if err != nil {
			fmt.Fprintf(os.Stderr, "error building: %v\n", err)
			os.Exit(1)
		}
	} else {
		remoteBuild()
	}
}

func remoteBuild() {
	j, err := pbMarshaler.MarshalToString(&cliBuildRequest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error marshaling request: %v\n", err)
		os.Exit(1)
	}
	url := fmt.Sprintf("%v/build", remoteURL)
	headers := map[string]string{"Content-Type": "application/json"}
	r, err := httpreq.HTTPRequest(url, "POST", bytes.NewBuffer([]byte(j)), headers, true)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error triggering remote build: %v\n", err)
		os.Exit(1)
	}
	resp := BuildRequestResponse{}
	err = jsonpb.Unmarshal(bytes.NewBuffer(r.BodyBytes), &resp)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error unmarshaling response: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("build_id: %v\n", resp.BuildId)
}
