/*
This package implements a Furan RPC client that can be directly imported by other Go programs.
It uses Consul service discovery to pick a random node.
*/

package rpcclient

import (
	"fmt"
	"io"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	consul "github.com/hashicorp/consul/api"
)

const (
	connTimeoutSecs = 30
)

//go:generate stringer -type=NodeSelectionStrategy

//NodeSelectionStrategy enumerates the ways that rpcclient will use to pick a node
type NodeSelectionStrategy int

const (
	RandomNodeSelection NodeSelectionStrategy = iota // Choose a random node
	NetworkProximity                                 // Pick the closest node as determined by Consul
)

// ImageBuildPusher describes an object capable of building and pushing container images
type ImageBuildPusher interface {
	Build(context.Context, chan *BuildEvent, *BuildRequest) (string, error)
}

// FuranClient is an object which issues remote RPC calls to a Furan server
type FuranClient struct {
	n      furanNode
	logger *log.Logger
}

// DiscoveryOptions describes the options for determining the Furan node to use
// for the client.
type DiscoveryOptions struct {
	UseConsul         bool                  // Whether to use Consul service discovery
	ConsulAddr        string                // Consul address to use (defaults to '127.0.0.1:8500')
	SelectionStrategy NodeSelectionStrategy // If UseConsul is true, this specifies the strategy for node selection
	ServiceName       string                // Required if UseConsul is true
	NodeList          []string              // Required if UseConsul is false. Nodes in the format "{host}:{port}". A random host will be used if len(NodeList) > 1
}

type furanNode struct {
	addr string
	port int
}

// NewFuranClient takes a Consul service name and returns a client which connects
// to a randomly chosen Furan host and uses the optional logger
func NewFuranClient(opts *DiscoveryOptions, logger *log.Logger) (*FuranClient, error) {
	fc := &FuranClient{}
	if logger == nil {
		fc.logger = log.New(os.Stderr, "", log.LstdFlags)
	} else {
		fc.logger = logger
	}
	if opts.UseConsul {
		if opts.ServiceName == "" {
			return nil, fmt.Errorf("ConsulService is required if UseConsul is true")
		}
	} else {
		if len(opts.NodeList) == 0 {
			return nil, fmt.Errorf("non-empty NodeList is required if UseConsul is false")
		}
		opts.SelectionStrategy = RandomNodeSelection
	}
	err := fc.init(opts)
	return fc, err
}

func (fc *FuranClient) init(opts *DiscoveryOptions) error {
	nodes := []furanNode{}
	if opts.UseConsul {
		cc := consul.DefaultConfig()
		if opts.ConsulAddr != "" {
			cc.Address = opts.ConsulAddr
		}
		c, err := consul.NewClient(cc)
		if err != nil {
			return err
		}
		qopts := &consul.QueryOptions{}
		if opts.SelectionStrategy == NetworkProximity {
			qopts.Near = "_agent"
		}
		fc.logger.Printf("connecting to Consul on %v", cc.Address)
		se, _, err := c.Health().Service(opts.ServiceName, "", true, qopts)
		if err != nil {
			return err
		}
		if len(se) == 0 {
			return fmt.Errorf("no Furan hosts found via Consul")
		}
		fc.logger.Printf("found %v Furan hosts", len(se))
		for _, s := range se {
			n := furanNode{
				addr: s.Node.Address,
				port: s.Service.Port,
			}
			nodes = append(nodes, n)
		}
	} else {
		for _, s := range opts.NodeList {
			ns := strings.Split(s, ":")
			if len(ns) != 2 {
				return fmt.Errorf("malformed node: %v", s)
			}
			p, err := strconv.Atoi(ns[1])
			if err != nil {
				return fmt.Errorf("malformed port: %v", s)
			}
			n := furanNode{
				addr: ns[0],
				port: p,
			}
			nodes = append(nodes, n)
		}
	}
	if opts.SelectionStrategy == RandomNodeSelection && len(nodes) > 1 {
		i, err := randomRange(len(nodes)) // Random node
		if err != nil {
			return err
		}
		fc.n = nodes[i]
	} else {
		fc.n = nodes[0]
	}
	fc.logger.Printf("using node %v", fc.n.addr)
	return nil
}

func (fc FuranClient) validateBuildRequest(req *BuildRequest) error {
	if req.Build.GithubRepo == "" {
		return fmt.Errorf("Build.GithubRepo is required")
	}
	if req.Build.Ref == "" {
		return fmt.Errorf("Build.Ref is required")
	}
	if len(req.Build.Tags) == 0 {
		return fmt.Errorf("at least one tag is required") // no tags causes datalayer failure
	}
	if req.Push.Registry.Repo == "" &&
		req.Push.S3.Region == "" &&
		req.Push.S3.Bucket == "" &&
		req.Push.S3.KeyPrefix == "" {
		return fmt.Errorf("you must specify either a Docker registry or S3 region/bucket/key-prefix as a push target")
	}
	return nil
}

func (fc FuranClient) rpcerr(err error, msg string, params ...interface{}) error {
	code := grpc.Code(err)
	msg = fmt.Sprintf(msg, params...)
	return fmt.Errorf("rpc error: %v: %v: %v", msg, code.String(), err)
}

// Build starts and monitors a build synchronously, sending BuildEvents to out.
// Returns an error if there was an RPC error or if the build/push fails
// You must read from out (or provide a sufficiently buffered channel) to prevent Build from blocking forever
func (fc FuranClient) Build(ctx context.Context, out chan *BuildEvent, req *BuildRequest) (string, error) {
	err := fc.validateBuildRequest(req)
	if err != nil {
		return "", err
	}

	remoteHost := fmt.Sprintf("%v:%v", fc.n.addr, fc.n.port)

	fc.logger.Printf("connecting to %v", remoteHost)

	conn, err := grpc.Dial(remoteHost, grpc.WithInsecure(), grpc.WithBlock(), grpc.WithTimeout(connTimeoutSecs*time.Second))
	if err != nil {
		return "", fmt.Errorf("error connecting to remote host: %v", err)
	}
	//defer conn.Close()

	c := NewFuranExecutorClient(conn)

	fc.logger.Printf("triggering build")
	resp, err := c.StartBuild(context.Background(), req)
	if err != nil {
		return "", fc.rpcerr(err, "StartBuild")
	}

	mreq := BuildStatusRequest{
		BuildId: resp.BuildId,
	}

	fc.logger.Printf("monitoring build: %v", resp.BuildId)
	stream, err := c.MonitorBuild(context.Background(), &mreq)
	if err != nil {
		return resp.BuildId, fc.rpcerr(err, "MonitorBuild")
	}

	for {
		event, err := stream.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			return resp.BuildId, fc.rpcerr(err, "stream.Recv")
		}
		out <- event
		if event.EventError.IsError {
			return resp.BuildId, fmt.Errorf("build error: %v", event.Message)
		}
	}

	return resp.BuildId, nil
}
