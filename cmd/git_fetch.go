package cmd

import (
	git "gopkg.in/libgit2/git2go.v23"
)

// GitFetcher represents a git repo fetcher/cloner
type GitFetcher struct {
}

func credsCallback(url string, username string, allowedTypes git.CredType) (git.ErrorCode, *git.Cred) {
	ret, cred := git.NewCredSshKey("git", gitConfig.pubKeyLocalPath, gitConfig.privKeyLocalPath, "")
	return git.ErrorCode(ret), &cred
}

func certificateCheckCallback(cert *git.Certificate, valid bool, hostname string) git.ErrorCode {
	return 0
}

// NewGitFetcher returns a new git fetcher
func NewGitFetcher() *GitFetcher {
	return &GitFetcher{}
}

// Clone clones the given repo (at branch) to the global checkout path
func (gf *GitFetcher) Clone(repo string, branch string) error {
	co := &git.CloneOptions{
		CheckoutBranch: branch,
		FetchOptions: &git.FetchOptions{
			RemoteCallbacks: git.RemoteCallbacks{
				CredentialsCallback: credsCallback,
			},
		},
		CheckoutOpts: &git.CheckoutOpts{
			Strategy: git.CheckoutForce,
		},
	}
	_, err := git.Clone(repo, gitConfig.checkoutPath, co)
	if err != nil {
		return err
	}
	return nil
}
