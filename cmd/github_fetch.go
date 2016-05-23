package cmd

import (
	"fmt"
	"net/url"

	"github.com/google/go-github/github"
	"golang.org/x/oauth2"
)

// RepoBuildData returns data from a repo necessary to do a Docker build
type RepoBuildData struct {
	DockerfileContents string
	ArchiveLink        *url.URL
}

// GitHubFetcher represents a github data fetcher
type GitHubFetcher struct {
	c *github.Client
}

// NewGitHubFetcher returns a new github fetcher
func NewGitHubFetcher(token string) *GitHubFetcher {
	ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: token})
	tc := oauth2.NewClient(oauth2.NoContext, ts)
	gf := &GitHubFetcher{
		c: github.NewClient(tc),
	}
	return gf
}

// Get fetches Dockerfile contents and gets an archive link for the repo
func (gf *GitHubFetcher) Get(owner string, repo string, dfPath string, ref string) (*RepoBuildData, error) {
	rbd := &RepoBuildData{}
	path := fmt.Sprintf("%v/Dockerfile", dfPath)
	opt := &github.RepositoryContentGetOptions{
		Ref: ref,
	}
	fc, _, _, err := gf.c.Repositories.GetContents(owner, repo, path, opt)
	if err != nil {
		return rbd, err
	}
	rbd.DockerfileContents = fc.String()
	url, _, err := gf.c.Repositories.GetArchiveLink(owner, repo, github.Tarball, opt)
	if err != nil {
		return rbd, err
	}
	rbd.ArchiveLink = url
	return rbd, nil
}
