package lib

import (
	"log"

	"github.com/dollarshaveclub/go-lib/cassandra"
)

// we need a separate explicit type for UDT that contains "cql:" tags
// (protobuf definitions can't have custom tags)
type buildRequestUDT struct {
	GithubRepo       string   `cql:"github_repo"`
	DockerfilePath   string   `cql:"dockerfile_path"`
	Tags             []string `cql:"tags"`
	TagWithCommitSha bool     `cql:"tag_with_commit_sha"`
	Ref              string   `cql:"ref"`
	PushRegistryRepo string   `cql:"push_registry_repo"`
	PushS3Region     string   `cql:"push_s3_region"`
	PushS3Bucket     string   `cql:"push_s3_bucket"`
	PushS3KeyPrefix  string   `cql:"push_s3_key_prefix"`
}

// UDTFromBuildRequest constructs a UDT struct from a BuildRequest
func udtFromBuildRequest(req *BuildRequest) *buildRequestUDT {
	return &buildRequestUDT{
		GithubRepo:       req.Build.GithubRepo,
		DockerfilePath:   req.Build.DockerfilePath,
		Tags:             req.Build.Tags,
		TagWithCommitSha: req.Build.TagWithCommitSha,
		Ref:              req.Build.Ref,
		PushRegistryRepo: req.Push.Registry.Repo,
		PushS3Region:     req.Push.S3.Region,
		PushS3Bucket:     req.Push.S3.Bucket,
		PushS3KeyPrefix:  req.Push.S3.KeyPrefix,
	}
}

// BuildRequestFromUDT constructs a BuildRequest from a UDT
func buildRequestFromUDT(udt *buildRequestUDT) *BuildRequest {
	br := &BuildRequest{
		Build: &BuildDefinition{},
		Push: &PushDefinition{
			Registry: &PushRegistryDefinition{},
			S3:       &PushS3Definition{},
		},
	}
	br.Build.GithubRepo = udt.GithubRepo
	br.Build.DockerfilePath = udt.DockerfilePath
	br.Build.Tags = udt.Tags
	br.Build.TagWithCommitSha = udt.TagWithCommitSha
	br.Build.Ref = udt.Ref
	br.Push.Registry.Repo = udt.PushRegistryRepo
	br.Push.S3.Region = udt.PushS3Region
	br.Push.S3.Bucket = udt.PushS3Bucket
	br.Push.S3.KeyPrefix = udt.PushS3KeyPrefix
	return br
}

// buildStateFromString returns the enum value from the string stored in the DB
func buildStateFromString(state string) BuildStatusResponse_BuildState {
	if val, ok := BuildStatusResponse_BuildState_value[state]; ok {
		return BuildStatusResponse_BuildState(val)
	}
	log.Fatalf("build state '%v' not found in enum! stale protobuf definition?", state)
	return BuildStatusResponse_BUILDING //unreachable
}

var requiredUDTs = []cassandra.UDT{
	cassandra.UDT{
		Name: "build_request",
		Columns: []string{
			"github_repo text",
			"dockerfile_path text",
			"tags list<text>",
			"tag_with_commit_sha boolean",
			"ref text",
			"push_registry_repo text",
			"push_s3_region text",
			"push_s3_bucket text",
			"push_s3_key_prefix text",
		},
	},
}
var requiredTables = []cassandra.CTable{
	cassandra.CTable{
		Name: "builds_by_id",
		Columns: []string{
			"id uuid PRIMARY KEY",
			"request frozen<build_request>",
			"state text",
			"build_output text",
			"push_output text",
			"finished boolean",
			"failed boolean",
			"cancelled boolean",
			"started timestamp",
			"completed timestamp",
			"duration double",
		},
	},
	cassandra.CTable{
		Name: "build_metrics_by_id",
		Columns: []string{
			"id uuid PRIMARY KEY",
			"docker_build_started timestamp",
			"docker_build_completed timestamp",
			"docker_build_duration double",
			"push_started timestamp",
			"push_completed timestamp",
			"push_duration double",
			"clean_started timestamp",
			"clean_completed timestamp",
			"clean_duration double",
			"docker_image_size bigint",
			"docker_image_vsize bigint",
		},
	},
	cassandra.CTable{
		Name: "build_events_by_id",
		Columns: []string{
			"id uuid PRIMARY KEY",
			"build_output list<blob>",
			"push_output list<blob>",
		},
	},
}
