package cmd

import (
	"fmt"
	"time"

	"github.com/gocql/gocql"
)

func createBuild(s *gocql.Session, req *BuildRequest) (*gocql.UUID, error) {
	q := `INSERT INTO builds_by_id (id, request, state, finished, failed, cancelled, started)
        VALUES (?,{github_repo: ?, dockerfile_path: ?, tags: ?, tag_with_commit_sha: ?, ref: ?,
					push_registry_repo: ?, push_s3_region: ?, push_s3_bucket: ?,
					push_s3_key_prefix: ?},?,?,?,?,?);`
	id, err := gocql.RandomUUID()
	if err != nil {
		return nil, err
	}
	udt := udtFromBuildRequest(req)
	err = s.Query(q, id, udt.GithubRepo, udt.DockerfilePath, udt.Tags, udt.TagWithCommitSha, udt.Ref,
		udt.PushRegistryRepo, udt.PushS3Region, udt.PushS3Bucket, udt.PushS3KeyPrefix,
		"started", false, false, false, time.Now()).Exec()
	if err != nil {
		return nil, err
	}
	q = `INSERT INTO build_metrics_by_id (id) VALUES (?);`
	return &id, s.Query(q, id).Exec()
}

func getBuildByID(s *gocql.Session, id gocql.UUID) (*BuildStatusResponse, error) {
	q := `SELECT request, state, build_output, push_output, finished, failed, cancelled,
        started, completed, duration FROM builds_by_id WHERE id = ?;`
	var udt buildRequestUDT
	var state string
	var started, completed time.Time
	bi := &BuildStatusResponse{
		BuildId: id.String(),
		Error:   &RPCError{},
	}
	err := s.Query(q, id).Scan(&udt.GithubRepo, &udt.DockerfilePath, &udt.Tags,
		&udt.TagWithCommitSha, &udt.Ref, &udt.PushRegistryRepo, &udt.PushS3Region,
		&udt.PushS3KeyPrefix, &udt.PushS3KeyPrefix, &state, &bi.BuildOutput,
		&bi.PushOutput, &bi.Finished, &bi.Failed, &bi.Cancelled, &started,
		&completed, &bi.Duration)
	if err != nil {
		return bi, err
	}
	bi.State = buildStateFromString(state)
	bi.BuildRequest = buildRequestFromUDT(&udt)
	bi.Started = started.Format(time.RFC3339)
	bi.Completed = completed.Format(time.RFC3339)
	return bi, nil
}

func setBuildFlags(s *gocql.Session, id gocql.UUID, flags map[string]bool) error {
	var err error
	q := `UPDATE builds_by_id SET %v = ? WHERE id = ?;`
	for k, v := range flags {
		err = s.Query(fmt.Sprintf(q, k), v, id).Exec()
		if err != nil {
			return err
		}
	}
	return nil
}

func setBuildCompletedTimestamp(s *gocql.Session, id gocql.UUID) error {
	var started time.Time
	now := time.Now()
	q := `SELECT started FROM builds_by_id WHERE id = ?;`
	err := s.Query(q, id).Scan(&started)
	if err != nil {
		return err
	}
	duration := now.Sub(started).Seconds()
	q = `UPDATE builds_by_id SET completed = ?, duration = ? WHERE id = ?;`
	return s.Query(q, now, duration, id).Exec()
}

func setBuildState(s *gocql.Session, id gocql.UUID, state BuildStatusResponse_BuildState) error {
	q := `UPDATE builds_by_id SET state = ? WHERE id = ?;`
	return s.Query(q, state.String(), id).Exec()
}

func setBuildImageBuildOutput(s *gocql.Session, id gocql.UUID, output []byte) error {
	q := `UPDATE builds_by_id SET build_output = ? WHERE id = ?;`
	return s.Query(q, string(output), id).Exec()
}

func setBuildPushOutput(s *gocql.Session, id gocql.UUID, output []byte) error {
	q := `UPDATE builds_by_id SET push_output = ? WHERE id = ?;`
	return s.Query(q, string(output), id).Exec()
}

// Only used in case of queue full when we can't actually do a build
func deleteBuild(s *gocql.Session, id gocql.UUID) error {
	q := `DELETE FROM builds_by_id WHERE id = ?;
	      DELETE FROM build_metrics_by_id WHERE id = ?;`
	return s.Query(q, id, id).Exec()
}

func setBuildTimeMetric(s *gocql.Session, id gocql.UUID, metric string) error {
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
	err := s.Query(fmt.Sprintf(q, metric), now, id).Exec()
	if err != nil {
		return err
	}
	if getstarted {
		q := `SELECT %v FROM build_metrics_by_id WHERE id = ?;`
		err := s.Query(fmt.Sprintf(q, startedcolumn), id).Scan(&started)
		if err != nil {
			return err
		}
		duration := now.Sub(started).Seconds()
		q = `UPDATE build_metrics_by_id SET %v = ? WHERE id = ?;`
		return s.Query(fmt.Sprintf(q, durationcolumn), duration, id).Exec()
	}
	return nil
}

func setDockerImageSizesMetric(s *gocql.Session, id gocql.UUID, size int64, vsize int64) error {
	q := `UPDATE build_metrics_by_id SET docker_image_size = ?, docker_image_vsize = ? WHERE id = ?;`
	return s.Query(q, size, vsize, id).Exec()
}
