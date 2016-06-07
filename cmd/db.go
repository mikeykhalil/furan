package cmd

import (
	"log"
	"strings"
	"time"

	"github.com/dollarshaveclub/go-lib/cassandra"
	"github.com/gocql/gocql"
	consul "github.com/hashicorp/consul/api"
)

const (
	keyspace               = "furan"
	replicationFactorPerDC = 2
)

type dbconfig struct {
	nodes             []string
	useConsul         bool
	consulServiceName string
	dataCenters       []string
	cluster           *gocql.ClusterConfig
	session           *gocql.Session
}

var dbConfig dbconfig

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
	log.Fatalf("build state '%v' not found in enum! state protobuf definition?", state)
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
}

// GetNodesFromConsul queries the local Consul agent for the given service,
// returning the healthy nodes in ascending order of network distance/latency
func getNodesFromConsul(svc string) ([]string, error) {
	nodes := []string{}
	c, err := consul.NewClient(consul.DefaultConfig())
	if err != nil {
		return nodes, err
	}
	h := c.Health()
	opts := &consul.QueryOptions{
		Near: "_agent",
	}
	se, _, err := h.Service(svc, "", true, opts)
	if err != nil {
		return nodes, err
	}
	for _, s := range se {
		nodes = append(nodes, s.Node.Address)
	}
	return nodes, nil
}

func connectToDB() {
	if dbConfig.useConsul {
		nodes, err := getNodesFromConsul(dbConfig.consulServiceName)
		if err != nil {
			log.Fatalf("error getting DB nodes: %v", err)
		}
		dbConfig.nodes = nodes
	}
	dbConfig.cluster = gocql.NewCluster(dbConfig.nodes...)
	dbConfig.cluster.ProtoVersion = 3
	dbConfig.cluster.NumConns = 20
	dbConfig.cluster.SocketKeepalive = 30 * time.Second
}

func setupDBSession() {
	s, err := dbConfig.cluster.CreateSession()
	if err != nil {
		log.Fatalf("error creating DB session: %v", err)
	}
	dbConfig.session = s
}

func initDB() {
	rfmap := map[string]uint{}
	for _, dc := range dbConfig.dataCenters {
		rfmap[dc] = replicationFactorPerDC
	}
	err := cassandra.CreateKeyspaceWithNetworkTopologyStrategy(dbConfig.cluster, keyspace, rfmap)
	if err != nil {
		log.Fatalf("error creating keyspace: %v", err)
	}
	err = cassandra.CreateRequiredTypes(dbConfig.cluster, requiredUDTs)
	if err != nil {
		log.Fatalf("error creating UDTs: %v", err)
	}
	err = cassandra.CreateRequiredTables(dbConfig.cluster, requiredTables)
	if err != nil {
		log.Fatalf("error creating tables: %v", err)
	}
}

func setupDB() {
	dbConfig.nodes = strings.Split(nodestr, ",")
	if !dbConfig.useConsul {
		if len(dbConfig.nodes) == 0 || dbConfig.nodes[0] == "" {
			log.Fatalf("cannot setup DB: Consul is disabled and node list is empty")
		}
	}
	dbConfig.dataCenters = strings.Split(datacenterstr, ",")
	connectToDB()
	initDB()
	setupDBSession()
}
