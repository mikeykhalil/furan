package cmd

import "time"

//go:generate stringer -type=buildStatus

type buildStatus int

const (
	building buildStatus = iota
	pushing
	pullingSquashed
	success
	buildFailure
	pushFailure
	pullSquashedFailure
)

type buildRequest struct {
	SourceRepo     string `json:"source_repo"`
	PrivateKeyPath string `json:"ssh_private_key_vault_path"`
	SourceBranch   string `json:"source_branch"`
	DockerRepo     string `json:"docker_repo"`
	Tag            string `json:"tag"`
	TagWithSHA     bool   `json:"tag_with_commit_sha"`
	PullSquashed   bool   `json:"pull_squashed_image"`
}

type requestResponse struct {
	BuildID string `json:"id"`
}

type buildStatusResponse struct {
	BuildID   string       `json:"id"`
	Request   buildRequest `json:"request"`
	state     buildStatus
	State     string `json:"state"`
	Failed    bool   `json:"failed"`
	started   time.Time
	Started   string `json:"started"`
	completed time.Time
	Completed string `json:"completed"`
	duration  uint64
	Duration  string `json:"duration"`
}
