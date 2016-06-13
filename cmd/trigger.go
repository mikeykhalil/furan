package cmd

import (
	"fmt"
	"io"
	"log"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/net/context"
	"google.golang.org/grpc"
)

const (
	connTimeoutSecs = 10
)

var remoteFuranHost string

// triggerCmd represents the trigger command
var triggerCmd = &cobra.Command{
	Use:   "trigger",
	Short: "Start a build on a remote Furan server",
	Long:  `Trigger and then monitor a build on a remote Furan server`,
	Run:   trigger,
}

func init() {
	triggerCmd.PersistentFlags().StringVar(&remoteFuranHost, "remote-host", "", "Remote Furan server with gRPC port (eg: furan.me.com:4001)")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.GithubRepo, "github-repo", "", "source github repo")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.Ref, "source-ref", "master", "source git ref")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.DockerfilePath, "dockerfile-path", "Dockerfile", "Dockerfile path (optional)")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.Registry.Repo, "image-repo", "", "push to image repo")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.Region, "s3-region", "", "S3 region")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.Bucket, "s3-bucket", "", "S3 bucket")
	triggerCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.KeyPrefix, "s3-key-prefix", "", "S3 key prefix")
	triggerCmd.PersistentFlags().StringVar(&tags, "tags", "master", "image tags (optional, comma-delimited)")
	triggerCmd.PersistentFlags().BoolVar(&cliBuildRequest.Build.TagWithCommitSha, "tag-sha", false, "additionally tag with git commit SHA (optional)")
	RootCmd.AddCommand(triggerCmd)
}

func rpcerr(err error, msg string, params ...interface{}) {
	code := grpc.Code(err)
	msg = fmt.Sprintf(msg, params...)
	clierr("rpc error: %v: %v: %v", msg, code.String(), err)
}

func trigger(cmd *cobra.Command, args []string) {
	if remoteFuranHost == "" {
		clierr("remote host is required")
	}
	validateCLIBuildRequest()

	log.Printf("connecting to %v", remoteFuranHost)
	conn, err := grpc.Dial(remoteFuranHost, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(connTimeoutSecs*time.Second))
	if err != nil {
		clierr("error connecting to remote host: %v", err)
	}
	defer conn.Close()

	c := NewFuranExecutorClient(conn)

	log.Printf("triggering build")
	resp, err := c.StartBuild(context.Background(), &cliBuildRequest)
	if err != nil {
		rpcerr(err, "StartBuild")
	}

	mreq := BuildStatusRequest{
		BuildId: resp.BuildId,
	}

	log.Printf("monitoring build: %v", resp.BuildId)
	stream, err := c.MonitorBuild(context.Background(), &mreq)
	if err != nil {
		rpcerr(err, "MonitorBuild")
	}
	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			rpcerr(err, "stream.Recv")
		}
		fmt.Println(event.Message)
	}
}
