package cmd

import (
	"log"
	"os"
	"testing"

	"github.com/gocql/gocql"
	"golang.org/x/net/context"
)

func TestBuild(t *testing.T) {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	ebp := NewMockEventBusProducer()
	dl := NewMockDataLayer()
	cf := NewMockCodeFetcher()
	ibc := NewMockImageBuildClient()
	ib, err := NewImageBuilder(ebp, dl, cf, ibc, mockDockercfg, logger)
	if err != nil {
		t.Fatalf("error creating image builder: %v", err)
	}
	req := BuildRequest{
		Build: &BuildDefinition{
			GithubRepo: "foobar/baz",
			Ref:        "master",
		},
		Push: &PushDefinition{
			S3:       &PushS3Definition{},
			Registry: &PushRegistryDefinition{},
		},
	}
	id, err := gocql.RandomUUID()
	if err != nil {
		t.Fatalf("error getting UUID: %v", err)
	}
	ctx := NewBuildIDContext(context.Background(), id)
	bid, err := ib.Build(ctx, &req, id)
	if err != nil {
		t.Fatalf("error in Build: %v", err)
	}
	t.Logf("build id: %v", bid)
}

func TestPushBuildToRegistry(t *testing.T) {
	logger := log.New(os.Stdout, "", log.LstdFlags)
	ebp := NewMockEventBusProducer()
	dl := NewMockDataLayer()
	cf := NewMockCodeFetcher()
	ibc := NewMockImageBuildClient()
	ib, err := NewImageBuilder(ebp, dl, cf, ibc, mockDockercfg, logger)
	if err != nil {
		t.Fatalf("error creating image builder: %v", err)
	}
	req := BuildRequest{
		Build: &BuildDefinition{
			GithubRepo: "foobar/baz",
			Ref:        "master",
		},
		Push: &PushDefinition{
			S3: &PushS3Definition{},
			Registry: &PushRegistryDefinition{
				Repo: "foobar/baz",
			},
		},
	}
	id, err := gocql.RandomUUID()
	if err != nil {
		t.Fatalf("error getting UUID: %v", err)
	}
	ctx := NewBuildIDContext(context.Background(), id)
	err = ib.PushBuildToRegistry(ctx, &req)
	if err != nil {
		log.Fatalf("error in PushBuildToRegistry: %v", err)
	}
}
