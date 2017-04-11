package cmd

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"

	docker "github.com/docker/engine-api/client"
	"github.com/dollarshaveclub/furan/lib"
	"github.com/spf13/cobra"
	"golang.org/x/net/context"
)

var buildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build and push a docker image from repo",
	Long: `Build a Docker image locally from the specified git repository and push
to the specified image repository or S3 target.

Set the following environment variables to allow access to your local Docker engine/daemon:

DOCKER_HOST
DOCKER_API_VERSION (optional)
DOCKER_TLS_VERIFY
DOCKER_CERT_PATH
`,
	PreRun: func(cmd *cobra.Command, args []string) {
		if buildS3ErrorLogs {
			if buildS3ErrorLogBucket == "" {
				clierr("S3 error log bucket must be defined")
			}
			if buildS3ErrorLogRegion == "" {
				clierr("S3 error log region must be defined")
			}
		}
	},
	Run: build,
}

var buildS3ErrorLogs bool
var buildS3ErrorLogRegion, buildS3ErrorLogBucket string
var buildS3ErrorLogsPresignTTL uint

func init() {
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.GithubRepo, "github-repo", "", "source github repo")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.Ref, "source-ref", "master", "source git ref")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Build.DockerfilePath, "dockerfile-path", "Dockerfile", "Dockerfile path (optional)")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.Registry.Repo, "image-repo", "", "push to image repo")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.Region, "s3-region", "", "S3 region")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.Bucket, "s3-bucket", "", "S3 bucket")
	buildCmd.PersistentFlags().StringVar(&cliBuildRequest.Push.S3.KeyPrefix, "s3-key-prefix", "", "S3 key prefix")
	buildCmd.PersistentFlags().StringVar(&tags, "tags", "master", "image tags (optional, comma-delimited)")
	buildCmd.PersistentFlags().BoolVar(&cliBuildRequest.Build.TagWithCommitSha, "tag-sha", false, "additionally tag with git commit SHA (optional)")
	buildCmd.PersistentFlags().BoolVar(&buildS3ErrorLogs, "s3-error-logs", false, "Upload failed build logs to S3 (region and bucket must be specified)")
	buildCmd.PersistentFlags().StringVar(&buildS3ErrorLogRegion, "s3-error-log-region", "us-west-2", "Region for S3 error log upload")
	buildCmd.PersistentFlags().StringVar(&buildS3ErrorLogBucket, "s3-error-log-bucket", "", "Bucket for S3 error log upload")
	buildCmd.PersistentFlags().UintVar(&buildS3ErrorLogsPresignTTL, "s3-error-log-presign-ttl", 60*4, "Presigned error log URL TTL in minutes (0 to disable)")
	RootCmd.AddCommand(buildCmd)
}

func validateCLIBuildRequest() {
	cliBuildRequest.Build.Tags = strings.Split(tags, ",")
	if cliBuildRequest.Push.Registry.Repo == "" &&
		cliBuildRequest.Push.S3.Region == "" &&
		cliBuildRequest.Push.S3.Bucket == "" &&
		cliBuildRequest.Push.S3.KeyPrefix == "" {
		clierr("you must specify either a Docker registry or S3 region/bucket/key-prefix as a push target")
	}
	if cliBuildRequest.Build.GithubRepo == "" {
		clierr("GitHub repo is required")
	}
	if cliBuildRequest.Build.Ref == "" {
		clierr("Source ref is required")
	}
}

func build(cmd *cobra.Command, args []string) {
	ctx, cancel := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt)
	go func() {
		for _ = range c {
			cancel()
			os.Exit(1)
			return
		}
	}()

	validateCLIBuildRequest()
	lib.setupVault()
	lib.setupDB(initializeDB)

	dnull, err := os.Open(os.DevNull)
	if err != nil {
		clierr("error opening %v: %v", os.DevNull, err)
	}
	defer dnull.Close()

	logger = log.New(dnull, "", log.LstdFlags)
	clogger := log.New(os.Stderr, "", log.LstdFlags)

	mc, err := lib.NewDatadogCollector(dogstatsdAddr)
	if err != nil {
		log.Fatalf("error creating Datadog collector: %v", err)
	}
	lib.setupKafka(mc)
	err = getDockercfg()
	if err != nil {
		clierr("Error getting dockercfg: %v", err)
	}

	gf := lib.NewGitHubFetcher(gitConfig.token)
	dc, err := docker.NewEnvClient()
	if err != nil {
		clierr("error creating Docker client: %v", err)
	}

	osm := NewS3StorageManager(awsConfig, mc, clogger)
	is := NewDockerImageSquasher(clogger)
	s3errcfg := S3ErrorLogConfig{
		PushToS3:          buildS3ErrorLogs,
		Region:            buildS3ErrorLogRegion,
		Bucket:            buildS3ErrorLogBucket,
		PresignTTLMinutes: buildS3ErrorLogsPresignTTL,
	}
	ib, err := NewImageBuilder(kafkaConfig.manager, dbConfig.datalayer, gf, dc, mc, osm, is, dockerConfig.dockercfgContents, s3errcfg, logger)
	if err != nil {
		clierr("error creating image builder: %v", err)
	}

	logger = log.New(dnull, "", log.LstdFlags)

	gs := NewGRPCServer(ib, dbConfig.datalayer, kafkaConfig.manager, kafkaConfig.manager, mc, 1, 1, logger)

	resp, err := gs.StartBuild(ctx, &cliBuildRequest)
	if err != nil {
		clierr("error running build: %v", err)
	}

	fmt.Fprintf(os.Stdout, "build id: %v\n", resp.BuildId)

	req := &BuildStatusRequest{
		BuildId: resp.BuildId,
	}

	ls := NewLocalServerStream(ctx, os.Stdout)
	err = gs.MonitorBuild(req, ls)
	if err != nil {
		clierr("error monitoring build: %v", err)
	}
}
