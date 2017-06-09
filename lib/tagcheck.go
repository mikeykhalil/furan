package lib

import (
	"fmt"
	"strings"

	"github.com/dollarshaveclub/go-lib/set"
	"github.com/heroku/docker-registry-client/registry"
)

// ImageTagChecker describes an object that can see if a tag exists for an image in a registry
type ImageTagChecker interface {
}

// RegistryTagChecker is an object that can check a remote registry for a set of tags
type RegistryTagChecker struct {
	dockercfg *Dockerconfig
}

// NewRegistryTagChecker returns a RegistryTagChecker using the specified dockercfg for authentication
func NewRegistryTagChecker(dockercfg *Dockerconfig) *RegistryTagChecker {
	return &RegistryTagChecker{
		dockercfg: dockercfg,
	}
}

// AllTagsExist checks a remote registry to see if all tags exist for the given repository.
// It returns the missing tags if any
func (rtc *RegistryTagChecker) AllTagsExist(tags []string, repo string) (bool, []string, error) {
	rs := strings.Split(repo, "/")
	if len(rs) != 3 {
		return false, nil, fmt.Errorf("bad format for repo: expected [host]/[namespace]/[repository]: %v", repo)
	}
	host := rs[0]
	ac, ok := rtc.dockercfg.DockercfgContents[host]
	if !ok {
		return false, nil, fmt.Errorf("auth for host not found in dockercfg: %v", host)
	}
	reg, err := registry.New(host, ac.Username, ac.Password)
	if err != nil {
		return false, nil, fmt.Errorf("error setting up registry client: %v: %v", repo, err)
	}
	ts, err := reg.Tags(fmt.Sprintf("%v/%v", rs[1], rs[2]))
	if err != nil {
		return false, nil, fmt.Errorf("error getting tags for repo: %v: %v", repo, err)
	}
	lset := set.NewStringSet(tags)
	inter := set.NewStringSet(ts).Intersection(lset)
	return lset.IsEqual(inter), lset.Difference(inter).Items(), nil
}
