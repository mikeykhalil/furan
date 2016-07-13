/*
This package implements a Furan RPC client that can be directly imported by other Go programs.
It uses Consul service discovery to pick a random node.
TODO: Implement optional node selection by network latency
*/

package rpcclient

import (
	"fmt"
	"io"
	"log"
	"os"
	"time"

	"google.golang.org/grpc"

	"golang.org/x/net/context"

	consul "github.com/hashicorp/consul/api"
)

const (
	connTimeoutSecs = 30
)

// ImageBuilder describes an object capable of building and pushing container images
type ImageBuildPusher interface {
	Build(context.Context, chan *BuildEvent, *BuildRequest) (string, error)
}

// FuranClient is an object which issues remote RPC calls to a Furan server
type FuranClient struct {
	n      *furanNode
	logger *log.Logger
}

type furanNode struct {
	addr string
	port int
}

// NewFuranClient takes a Consul service name and returns a client which connects
// to a randomly chosen Furan host and uses the optional logger
func NewFuranClient(svc string, logger *log.Logger) (*FuranClient, error) {
	fc := &FuranClient{}
	if logger == nil {
		fc.logger = log.New(os.Stderr, "", log.LstdFlags)
	} else {
		fc.logger = logger
	}
	err := fc.init(svc)
	return fc, err
}

func (fc *FuranClient) init(svc string) error {
	nodes := []furanNode{}
	c, err := consul.NewClient(consul.DefaultConfig())
	if err != nil {
		return err
	}
	fc.logger.Printf("connecting to Consul on localhost")
	se, _, err := c.Health().Service(svc, "", true, &consul.QueryOptions{})
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
	i, err := randomRange(len(nodes) - 1) // Random node
	if err != nil {
		return err
	}
	fc.n = &nodes[i]
	return nil
}

func (fc *FuranClient) validateBuildRequest(req *BuildRequest) error {
	if req.Build.GithubRepo == "" {
		return fmt.Errorf("Build.GithubRepo is required")
	}
	if req.Build.Ref == "" {
		return fmt.Errorf("Build.Ref is required")
	}
	if req.Push.Registry.Repo == "" &&
		req.Push.S3.Region == "" &&
		req.Push.S3.Bucket == "" &&
		req.Push.S3.KeyPrefix == "" {
		return fmt.Errorf("you must specify either a Docker registry or S3 region/bucket/key-prefix as a push target")
	}
	return nil
}

func (fc *FuranClient) rpcerr(err error, msg string, params ...interface{}) error {
	code := grpc.Code(err)
	msg = fmt.Sprintf(msg, params...)
	return fmt.Errorf("rpc error: %v: %v: %v", msg, code.String(), err)
}

// Build starts and monitors a build synchronously, sending BuildEvents to out.
// Returns an error if there was an RPC error or if the build/push fails
// You must read from out (or provide a sufficiently buffered channel) to prevent Build from blocking forever
func (fc *FuranClient) Build(ctx context.Context, out chan *BuildEvent, req *BuildRequest) (string, error) {
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
	defer conn.Close()

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
	stream, err := c.MonitorBuild(ctx, &mreq) // only pass through ctx to monitor call so user can cancel
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
