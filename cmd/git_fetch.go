package cmd

import (
	git "gopkg.in/libgit2/git2go.v23"
)

type GitFetcher struct {
}

func NewGitFetcher() (*GitFetcher, error) {
	_ = git.BranchLocal
	return &GitFetcher{}, nil
}
