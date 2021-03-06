package datalayer

import (
	"fmt"
	"time"

	"github.com/dollarshaveclub/furan/generated/lib"
	"github.com/dollarshaveclub/furan/lib/db"
	"github.com/gocql/gocql"
	"github.com/golang/protobuf/proto"
	newrelic "github.com/newrelic/go-agent"
)

// DataLayer describes an object that interacts with the persistant data store
type DataLayer interface {
	CreateBuild(newrelic.Transaction, *lib.BuildRequest) (gocql.UUID, error)
	GetBuildByID(newrelic.Transaction, gocql.UUID) (*lib.BuildStatusResponse, error)
	SetBuildFlags(newrelic.Transaction, gocql.UUID, map[string]bool) error
	SetBuildCompletedTimestamp(newrelic.Transaction, gocql.UUID) error
	SetBuildState(newrelic.Transaction, gocql.UUID, lib.BuildStatusResponse_BuildState) error
	DeleteBuild(newrelic.Transaction, gocql.UUID) error
	SetBuildTimeMetric(newrelic.Transaction, gocql.UUID, string) error
	SetDockerImageSizesMetric(newrelic.Transaction, gocql.UUID, int64, int64) error
	SaveBuildOutput(newrelic.Transaction, gocql.UUID, []lib.BuildEvent, string) error
	GetBuildOutput(newrelic.Transaction, gocql.UUID, string) ([]lib.BuildEvent, error)
}

// DBLayer is an DataLayer instance that interacts with the Cassandra database
type DBLayer struct {
	s *gocql.Session
}

// NewDBLayer returns a data layer object
func NewDBLayer(s *gocql.Session) *DBLayer {
	return &DBLayer{s: s}
}

// CreateBuild inserts a new build into the DB returning the ID
func (dl *DBLayer) CreateBuild(txn newrelic.Transaction, req *lib.BuildRequest) (gocql.UUID, error) {
	ds := newrelic.DatastoreSegment{
		StartTime:  newrelic.StartSegmentNow(txn),
		Product:    newrelic.DatastoreCassandra,
		Collection: "builds_by_id",
		Operation:  "INSERT",
	}
	defer ds.End()

	q := `INSERT INTO builds_by_id (id, request, state, finished, failed, cancelled, started)
        VALUES (?,{github_repo: ?, dockerfile_path: ?, tags: ?, tag_with_commit_sha: ?, ref: ?,
					push_registry_repo: ?, push_s3_region: ?, push_s3_bucket: ?,
					push_s3_key_prefix: ?},?,?,?,?,?);`
	id, err := gocql.RandomUUID()
	if err != nil {
		return id, err
	}
	udt := db.UDTFromBuildRequest(req)
	err = dl.s.Query(q, id, udt.GithubRepo, udt.DockerfilePath, udt.Tags, udt.TagWithCommitSha, udt.Ref,
		udt.PushRegistryRepo, udt.PushS3Region, udt.PushS3Bucket, udt.PushS3KeyPrefix,
		lib.BuildStatusResponse_STARTED.String(), false, false, false, time.Now()).Exec()
	if err != nil {
		return id, err
	}

	ds.End()
	ds.Collection = "build_metrics_by_id"
	ds.StartTime = newrelic.StartSegmentNow(txn)

	q = `INSERT INTO build_metrics_by_id (id) VALUES (?);`
	err = dl.s.Query(q, id).Exec()
	if err != nil {
		return id, err
	}
	q = `INSERT INTO build_events_by_id (id) VALUES (?);`
	return id, dl.s.Query(q, id).Exec()
}

// GetBuildByID fetches a build object from the DB
func (dl *DBLayer) GetBuildByID(txn newrelic.Transaction, id gocql.UUID) (*lib.BuildStatusResponse, error) {
	defer newrelic.DatastoreSegment{
		StartTime:  newrelic.StartSegmentNow(txn),
		Product:    newrelic.DatastoreCassandra,
		Collection: "builds_by_id",
		Operation:  "SELECT",
	}.End()

	q := `SELECT request, state, finished, failed, cancelled, started, completed,
	      duration FROM builds_by_id WHERE id = ?;`
	var udt db.BuildRequestUDT
	var state string
	var started, completed time.Time
	bi := &lib.BuildStatusResponse{
		BuildId: id.String(),
	}
	err := dl.s.Query(q, id).Scan(&udt, &state, &bi.Finished, &bi.Failed,
		&bi.Cancelled, &started, &completed, &bi.Duration)
	if err != nil {
		return bi, err
	}
	bi.State = db.BuildStateFromString(state)
	bi.BuildRequest = db.BuildRequestFromUDT(&udt)
	bi.Started = started.Format(time.RFC3339)
	bi.Completed = completed.Format(time.RFC3339)
	return bi, nil
}

// SetBuildFlags sets the boolean flags on the build object
// Caller must ensure that the flags passed in are valid
func (dl *DBLayer) SetBuildFlags(txn newrelic.Transaction, id gocql.UUID, flags map[string]bool) error {
	defer newrelic.DatastoreSegment{
		StartTime:  newrelic.StartSegmentNow(txn),
		Product:    newrelic.DatastoreCassandra,
		Collection: "builds_by_id",
		Operation:  "UPDATE",
	}.End()

	var err error
	q := `UPDATE builds_by_id SET %v = ? WHERE id = ?;`
	for k, v := range flags {
		err = dl.s.Query(fmt.Sprintf(q, k), v, id).Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

// SetBuildCompletedTimestamp sets the completed timestamp on a build to time.Now()
func (dl *DBLayer) SetBuildCompletedTimestamp(txn newrelic.Transaction, id gocql.UUID) error {
	defer newrelic.DatastoreSegment{
		StartTime:  newrelic.StartSegmentNow(txn),
		Product:    newrelic.DatastoreCassandra,
		Collection: "builds_by_id",
		Operation:  "UPDATE",
	}.End()

	var started time.Time
	now := time.Now()
	q := `SELECT started FROM builds_by_id WHERE id = ?;`
	err := dl.s.Query(q, id).Scan(&started)
	if err != nil {
		return err
	}
	duration := now.Sub(started).Seconds()
	q = `UPDATE builds_by_id SET completed = ?, duration = ? WHERE id = ?;`
	return dl.s.Query(q, now, duration, id).Exec()
}

// SetBuildState sets the state of a build
func (dl *DBLayer) SetBuildState(txn newrelic.Transaction, id gocql.UUID, state lib.BuildStatusResponse_BuildState) error {
	q := `UPDATE builds_by_id SET state = ? WHERE id = ?;`
	return dl.s.Query(q, state.String(), id).Exec()
}

// DeleteBuild removes a build from the DB.
// Only used in case of queue full when we can't actually do a build
func (dl *DBLayer) DeleteBuild(txn newrelic.Transaction, id gocql.UUID) error {
	ds := newrelic.DatastoreSegment{
		StartTime:  newrelic.StartSegmentNow(txn),
		Product:    newrelic.DatastoreCassandra,
		Collection: "builds_by_id",
		Operation:  "DELETE",
	}
	defer ds.End()

	q := `DELETE FROM builds_by_id WHERE id = ?;`
	err := dl.s.Query(q, id).Exec()
	if err != nil {
		return err
	}

	ds.End()
	ds.Collection = "build_metrics_by_id"
	ds.StartTime = newrelic.StartSegmentNow(txn)

	q = `DELETE FROM build_metrics_by_id WHERE id = ?;`
	return dl.s.Query(q, id).Exec()
}

// SetBuildTimeMetric sets a build metric to time.Now()
// metric is the name of the column to update
// if metric is a *_completed column, it will also compute and persist the duration
func (dl *DBLayer) SetBuildTimeMetric(txn newrelic.Transaction, id gocql.UUID, metric string) error {
	ds := newrelic.DatastoreSegment{
		StartTime:  newrelic.StartSegmentNow(txn),
		Product:    newrelic.DatastoreCassandra,
		Collection: "build_metrics_by_id",
		Operation:  "UPDATE",
	}
	defer ds.End()

	var started time.Time
	now := time.Now()
	getstarted := true
	var startedcolumn string
	var durationcolumn string
	switch metric {
	case "docker_build_completed":
		startedcolumn = "docker_build_started"
		durationcolumn = "docker_build_duration"
	case "push_completed":
		startedcolumn = "push_started"
		durationcolumn = "push_duration"
	case "clean_completed":
		startedcolumn = "clean_started"
		durationcolumn = "clean_duration"
	default:
		getstarted = false
	}
	q := `UPDATE build_metrics_by_id SET %v = ? WHERE id = ?;`
	err := dl.s.Query(fmt.Sprintf(q, metric), now, id).Exec()
	if err != nil {
		return err
	}
	if getstarted {

		ds.End()
		ds.Operation = "SELECT"
		ds.StartTime = newrelic.StartSegmentNow(txn)

		q := `SELECT %v FROM build_metrics_by_id WHERE id = ?;`
		err := dl.s.Query(fmt.Sprintf(q, startedcolumn), id).Scan(&started)
		if err != nil {
			return err
		}
		duration := now.Sub(started).Seconds()

		ds.End()
		ds.Operation = "UPDATE"
		ds.StartTime = newrelic.StartSegmentNow(txn)

		q = `UPDATE build_metrics_by_id SET %v = ? WHERE id = ?;`
		return dl.s.Query(fmt.Sprintf(q, durationcolumn), duration, id).Exec()
	}
	return nil
}

// SetDockerImageSizesMetric sets the docker image sizes for a build
func (dl *DBLayer) SetDockerImageSizesMetric(txn newrelic.Transaction, id gocql.UUID, size int64, vsize int64) error {
	defer newrelic.DatastoreSegment{
		StartTime:  newrelic.StartSegmentNow(txn),
		Product:    newrelic.DatastoreCassandra,
		Collection: "build_metrics_by_id",
		Operation:  "UPDATE",
	}.End()

	q := `UPDATE build_metrics_by_id SET docker_image_size = ?, docker_image_vsize = ? WHERE id = ?;`
	return dl.s.Query(q, size, vsize, id).Exec()
}

// SaveBuildOutput serializes an array of stream events to the database
func (dl *DBLayer) SaveBuildOutput(txn newrelic.Transaction, id gocql.UUID, output []lib.BuildEvent, column string) error {
	defer newrelic.DatastoreSegment{
		StartTime:  newrelic.StartSegmentNow(txn),
		Product:    newrelic.DatastoreCassandra,
		Collection: "build_events_by_id",
		Operation:  "UPDATE",
	}.End()

	serialized := make([][]byte, len(output))
	var err error
	var b []byte
	for i, e := range output {
		b, err = proto.Marshal(&e)
		if err != nil {
			return err
		}
		serialized[i] = b
	}
	q := `UPDATE build_events_by_id SET %v = ? WHERE id = ?;`
	return dl.s.Query(fmt.Sprintf(q, column), serialized, id.String()).Exec()
}

// GetBuildOutput returns an array of stream events from the database
func (dl *DBLayer) GetBuildOutput(txn newrelic.Transaction, id gocql.UUID, column string) ([]lib.BuildEvent, error) {
	defer newrelic.DatastoreSegment{
		StartTime:  newrelic.StartSegmentNow(txn),
		Product:    newrelic.DatastoreCassandra,
		Collection: "build_events_by_id",
		Operation:  "SELECT",
	}.End()

	var rawoutput [][]byte
	output := []lib.BuildEvent{}
	q := `SELECT %v FROM build_events_by_id WHERE id = ?;`
	err := dl.s.Query(fmt.Sprintf(q, column), id).Scan(&rawoutput)
	if err != nil {
		return output, err
	}
	for _, rawevent := range rawoutput {
		event := lib.BuildEvent{}
		err = proto.Unmarshal(rawevent, &event)
		if err != nil {
			return output, err
		}
		output = append(output, event)
	}
	return output, nil
}
