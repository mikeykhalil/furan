package consul

import (
	"testing"

	"github.com/dollarshaveclub/furan/lib/config"
	"github.com/dollarshaveclub/furan/lib/mocks"
	"github.com/gocql/gocql"
	"github.com/golang/mock/gomock"
	consul "github.com/hashicorp/consul/api"
)

var testConsulconfig = &config.Consulconfig{
	KVPrefix: "/furan",
}

/*
type KeyValueOrchestrator interface {
	SetBuildRunning(id gocql.UUID) error
	DeleteBuildRunning(id gocql.UUID) error
	SetBuildCancelled(id gocql.UUID) error
	DeleteBuildCancelled(id gocql.UUID) error
	CheckIfBuildRunning(id gocql.UUID) (bool, error)
	WatchIfBuildStopsRunning(id gocql.UUID, timeout time.Duration) (bool, error)
	WatchIfBuildCancelled(id gocql.UUID, timeout time.Duration) (bool, error)
}
*/

func TestSetBuildRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	mconsul.EXPECT().Put(gomock.Eq(&consul.KVPair{Key: testConsulconfig.KVPrefix + "/running/" + id.String()}), nil).Times(1)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	err = cvo.SetBuildRunning(id)
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
}

func TestDeleteBuildRunning(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	mconsul.EXPECT().Delete(gomock.Eq(testConsulconfig.KVPrefix+"/running/"+id.String()), nil).Times(1)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	err = cvo.DeleteBuildRunning(id)
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
}

func TestSetBuildCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	mconsul.EXPECT().Put(gomock.Eq(&consul.KVPair{Key: testConsulconfig.KVPrefix + "/cancelled/" + id.String()}), nil).Times(1)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	err = cvo.SetBuildCancelled(id)
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
}

func TestDeleteBuildCancelled(t *testing.T) {
	ctrl := gomock.NewController(t)
	mconsul := mocks.NewMockConsulKV(ctrl)
	defer ctrl.Finish()
	id, _ := gocql.RandomUUID()
	mconsul.EXPECT().Delete(gomock.Eq(testConsulconfig.KVPrefix+"/cancelled/"+id.String()), nil).Times(1)
	cvo, err := newConsulKVOrchestrator(mconsul, testConsulconfig)
	if err != nil {
		t.Fatalf("error creating consul orchestrator: %v", err)
	}
	err = cvo.DeleteBuildCancelled(id)
	if err != nil {
		t.Fatalf("should have succeeded: %v", err)
	}
}
