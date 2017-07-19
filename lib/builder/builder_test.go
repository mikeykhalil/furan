package builder

import (
	"context"
	"io/ioutil"
	"log"
	"strings"
	"testing"

	dtypes "github.com/docker/engine-api/types"
	"github.com/dollarshaveclub/furan/generated/pb"
	"github.com/dollarshaveclub/furan/lib/mocks"
	"github.com/gocql/gocql"
	"github.com/golang/mock/gomock"
)

var testLogger = log.New(ioutil.Discard, "", log.LstdFlags)
var testDockerCfg = map[string]dtypes.AuthConfig{}
var testS3ErrorLogcfg = S3ErrorLogConfig{}

type imageBuildPusherDeps struct {
	ctrl *gomock.Controller
	mdl  *mocks.MockDataLayer
	mcf  *mocks.MockCodeFetcher
	mebp *mocks.MockEventBusProducer
	mmc  *mocks.MockMetricsCollector
	mis  *mocks.MockImageSquasher
	mibc *mocks.MockImageBuildClient
	mitc *mocks.MockImageTagChecker
	mosm *mocks.MockObjectStorageManager
}

func getTestImageBuildPusher(t *testing.T) (ImageBuildPusher, *imageBuildPusherDeps, *gomock.Controller) {
	ctrl := gomock.NewController(t)
	deps := imageBuildPusherDeps{
		mdl:  mocks.NewMockDataLayer(ctrl),
		mcf:  mocks.NewMockCodeFetcher(ctrl),
		mebp: mocks.NewMockEventBusProducer(ctrl),
		mmc:  mocks.NewMockMetricsCollector(ctrl),
		mis:  mocks.NewMockImageSquasher(ctrl),
		mibc: mocks.NewMockImageBuildClient(ctrl),
		mitc: mocks.NewMockImageTagChecker(ctrl),
		mosm: mocks.NewMockObjectStorageManager(ctrl),
	}
	ibp, err := NewImageBuilder(deps.mebp, deps.mdl, deps.mcf, deps.mibc, deps.mmc, deps.mosm, deps.mis, deps.mitc, testDockerCfg, testS3ErrorLogcfg, testLogger)
	if err != nil {
		t.Fatalf("error getting ImageBuilder: %v", err)
	}
	return ibp, &deps, ctrl
}

func TestImageBuildTagCheckRegistrySkip(t *testing.T) {
	ibp, deps, ctrl := getTestImageBuildPusher(t)
	defer ctrl.Finish()

	ctx := context.Background()
	id, _ := gocql.RandomUUID()

	deps.mdl.EXPECT().SetBuildTimeMetric(id, gomock.Any()).Times(1)
	deps.mcf.EXPECT().GetCommitSHA("dollarshaveclub", "furan", "master").Return("asdf1234", nil).Times(1)
	deps.mitc.EXPECT().AllTagsExist([]string{"master"}, "quay.io/dollarshaveclub/furan").Times(1).Return(true, nil, nil)

	req := &pb.BuildRequest{
		SkipIfExists: true,
		Build: &pb.BuildDefinition{
			GithubRepo: "dollarshaveclub/furan",
			Ref:        "master",
			Tags:       []string{"master"},
		},
		Push: &pb.PushDefinition{
			Registry: &pb.PushRegistryDefinition{
				Repo: "quay.io/dollarshaveclub/furan",
			},
		},
	}

	_, err := ibp.Build(ctx, req, id)
	if err == nil {
		t.Fatalf("build should have been skipped")
	}
	if !strings.Contains(err.Error(), "build not necessary") {
		t.Fatalf("build error should have said not necessary")
	}
}

func TestImageBuildTagCheckS3Skip(t *testing.T) {
	ibp, deps, ctrl := getTestImageBuildPusher(t)
	defer ctrl.Finish()

	ctx := context.Background()
	id, _ := gocql.RandomUUID()

	deps.mdl.EXPECT().SetBuildTimeMetric(id, gomock.Any()).Times(1)
	deps.mcf.EXPECT().GetCommitSHA("dollarshaveclub", "furan", "master").Return("asdf1234", nil).Times(1)
	deps.mosm.EXPECT().Exists(gomock.Any(), gomock.Any()).Times(1).Return(true, nil)

	req := &pb.BuildRequest{
		SkipIfExists: true,
		Build: &pb.BuildDefinition{
			GithubRepo: "dollarshaveclub/furan",
			Ref:        "master",
			Tags:       []string{"master"},
		},
		Push: &pb.PushDefinition{
			Registry: &pb.PushRegistryDefinition{},
			S3: &pb.PushS3Definition{
				Region:    "us-west-2",
				Bucket:    "foo",
				KeyPrefix: "bar",
			},
		},
	}

	_, err := ibp.Build(ctx, req, id)
	if err == nil {
		t.Fatalf("build should have been skipped")
	}
	if !strings.Contains(err.Error(), "build not necessary") {
		t.Fatalf("build error should have said not necessary")
	}
}
