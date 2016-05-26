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

var requiredUDTs = []cassandra.UDT{
	cassandra.UDT{
		Name: "build_request",
		Columns: []string{
			"github_repo text",
			"tags set<text>",
			"tag_with_commit_sha boolean",
			"ref_type text",
			"ref text",
			"push_registry_repo text",
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
			"finished boolean",
			"failed boolean",
			"cancelled boolean",
			"started timestamp",
			"completed timestamp",
			"duration int",
		},
	},
	// Builds by source repo/sha and destination
	// if a build exists in this table (and--if push target is s3--it also exists there)
	// we don't need to run the build again
	cassandra.CTable{
		Name: "builds_by_request_params",
		Columns: []string{
			"github_repo text",
			"commit_sha text",
			"push_registry_repo text",
			"push_s3_bucket text",
			"push_s3_key_prefix text",
			"build_id uuid",
			"state text",
			"finished boolean",
			"failed boolean",
			"cancelled boolean",
			"request frozen<build_request>",
			"PRIMARY KEY (commit_sha, github_repo, push_registry_repo, push_s3_bucket, push_s3_key_prefix)",
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
