package cmd

import (
	"time"

	"github.com/gocql/gocql"
)

func createBuild(s *gocql.Session, req *BuildRequest) (*gocql.UUID, error) {
	q := `INSERT INTO builds_by_id (id, request, state, finished, failed, cancelled, started)
        VALUES (?,?,?,?,?,?,?);`
	id, err := gocql.RandomUUID()
	if err != nil {
		return nil, err
	}
	return &id, s.Query(q, id, udtFromBuildRequest(req), "started", false, false, false, time.Now()).Exec()
}

func getBuildByID(s *gocql.Session, id gocql.UUID) (*BuildStatusResponse, error) {
	q := `SELECT request, state, build_output, push_output, finished, failed, cancelled,
        started, completed, duration FROM builds_by_id WHERE id = ?;`
	var udt buildRequestUDT
	var state string
	var started, completed time.Time
	bi := &BuildStatusResponse{BuildId: id.String()}
	err := s.Query(q, id).Scan(&udt, &state, &bi.BuildOutput, &bi.PushOutput,
		&bi.Finished, &bi.Failed, &bi.Cancelled, &started, &completed, &bi.Duration)
	if err != nil {
		return nil, err
	}
	bi.State = buildStateFromString(state)
	bi.BuildRequest = buildRequestFromUDT(&udt)
	bi.Started = started.Format(time.RFC3339)
	bi.Completed = completed.Format(time.RFC3339)
	return bi, nil
}

func setBuildFlags(s *gocql.Session, id gocql.UUID, flags map[string]bool) error {
	var err error
	q := `UPDATE builds_by_id SET ? = ? WHERE id = ?;`
	for k, v := range flags {
		err = s.Query(q, k, v, id).Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

func setBuildState(s *gocql.Session, id gocql.UUID, state BuildStatusResponse_BuildState) error {
	q := `UPDATE builds_by_id SET state = ? WHERE id = ?;`
	return s.Query(q, state.String(), id).Exec()
}

func setBuildImageBuildOutput(s *gocql.Session, id gocql.UUID, output []byte) error {
	q := `UPDATE build_by_id SET build_output = ? WHERE id = ?;`
	return s.Query(q, string(output), id).Exec()
}

func setBuildPushOutput(s *gocql.Session, id gocql.UUID, output []byte) error {
	q := `UPDATE build_by_id SET push_output = ? WHERE id = ?;`
	return s.Query(q, string(output), id).Exec()
}
