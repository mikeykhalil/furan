package cmd

import (
	"bytes"
	"io"

	"golang.org/x/net/context"

	dtypes "github.com/docker/engine-api/types"
	"github.com/gocql/gocql"
)

var mockDockercfg = map[string]dtypes.AuthConfig{
	"https://index.docker.io/v2/": dtypes.AuthConfig{
		Username: "asdf",
		Password: "lalala",
	},
}

// BufferCloser is bytes.Buffer that satisfies io.ReadCloser
type BufferCloser struct {
	bytes.Buffer
}

func NewBufferCloser(b []byte) *BufferCloser {
	return &BufferCloser{
		*bytes.NewBuffer(b),
	}
}
func (bc *BufferCloser) Close() error {
	return nil
}

type MockDataLayer struct {
}

func NewMockDataLayer() DataLayer {
	return &MockDataLayer{}
}

func (mdl *MockDataLayer) CreateBuild(req *BuildRequest) (gocql.UUID, error) {
	return gocql.RandomUUID()
}

func (mdl *MockDataLayer) GetBuildByID(id gocql.UUID) (*BuildStatusResponse, error) {
	resp := &BuildStatusResponse{}
	return resp, nil
}

func (mdl *MockDataLayer) SetBuildFlags(id gocql.UUID, flags map[string]bool) error {
	return nil
}

func (mdl *MockDataLayer) SetBuildCompletedTimestamp(id gocql.UUID) error {
	return nil
}

func (mdl *MockDataLayer) SetBuildState(id gocql.UUID, state BuildStatusResponse_BuildState) error {
	return nil
}

func (mdl *MockDataLayer) DeleteBuild(id gocql.UUID) error {
	return nil
}

func (mdl *MockDataLayer) SetBuildTimeMetric(id gocql.UUID, column string) error {
	return nil
}

func (mdl *MockDataLayer) SetDockerImageSizesMetric(id gocql.UUID, size int64, vsize int64) error {
	return nil
}

func (mdl *MockDataLayer) SaveBuildOutput(id gocql.UUID, events []BuildEvent, column string) error {
	return nil
}

func (mdl *MockDataLayer) GetBuildOutput(id gocql.UUID, column string) ([]BuildEvent, error) {
	return []BuildEvent{}, nil
}

type MockEventBusProducer struct {
}

func NewMockEventBusProducer() *MockEventBusProducer {
	return &MockEventBusProducer{}
}

func (mebp *MockEventBusProducer) PublishEvent(*BuildEvent) error {
	return nil
}

type MockCodeFetcher struct {
}

func NewMockCodeFetcher() *MockCodeFetcher {
	return &MockCodeFetcher{}
}

func (mcf *MockCodeFetcher) Get(owner string, repo string, ref string) (io.Reader, error) {
	return bytes.NewBuffer([]byte{}), nil
}

type MockImageBuildClient struct {
}

func NewMockImageBuildClient() *MockImageBuildClient {
	return &MockImageBuildClient{}
}

func (mibc *MockImageBuildClient) ImageBuild(ctx context.Context, r io.Reader, opts dtypes.ImageBuildOptions) (dtypes.ImageBuildResponse, error) {
	resp := dtypes.ImageBuildResponse{}
	resp.Body = NewBufferCloser([]byte(`{"stream":"doing foo"}
    {"stream":"Successfully built xxxxx"}
    `))
	return resp, nil
}

func (mibc *MockImageBuildClient) ImageInspectWithRaw(ctx context.Context, id string, flag bool) (dtypes.ImageInspect, []byte, error) {
	return dtypes.ImageInspect{}, nil, nil
}

func (mibc *MockImageBuildClient) ImageRemove(ctx context.Context, id string, opts dtypes.ImageRemoveOptions) ([]dtypes.ImageDelete, error) {
	return []dtypes.ImageDelete{}, nil
}

func (mibc *MockImageBuildClient) ImagePush(ctx context.Context, tag string, opts dtypes.ImagePushOptions) (io.ReadCloser, error) {
	return &BufferCloser{}, nil
}
