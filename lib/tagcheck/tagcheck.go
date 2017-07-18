package tagcheck

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dollarshaveclub/furan/lib/config"
	"github.com/dollarshaveclub/go-lib/set"
	"github.com/heroku/docker-registry-client/registry"
)

// ImageTagChecker describes an object that can see if a tag exists for an image in a registry
type ImageTagChecker interface {
	AllTagsExist(tags []string, repo string) (bool, []string, error)
}

// RegistryTagChecker is an object that can check a remote registry for a set of tags
type RegistryTagChecker struct {
	dockercfg  *config.Dockerconfig
	loggerFunc func(string, ...interface{})
}

// NewRegistryTagChecker returns a RegistryTagChecker using the specified dockercfg for authentication
func NewRegistryTagChecker(dockercfg *config.Dockerconfig, loggerFunc func(string, ...interface{})) *RegistryTagChecker {
	return &RegistryTagChecker{
		dockercfg:  dockercfg,
		loggerFunc: loggerFunc,
	}
}

// AllTagsExist checks a remote registry to see if all tags exist for the given repository.
// It returns the missing tags if any
func (rtc *RegistryTagChecker) AllTagsExist(tags []string, repo string) (bool, []string, error) {
	rs := strings.Split(repo, "/")
	if len(rs) != 3 {
		if len(rs) != 2 {
			return false, nil, fmt.Errorf("bad format for repo: expected [host]/[namespace]/[repository] or [namespace]/[repository]: %v", repo)
		}
		rs = []string{"registry-1.docker.io", rs[0], rs[1]}
	}
	if len(tags) == 0 {
		return false, nil, fmt.Errorf("at least one tag is required")
	}
	hc := &http.Client{}
	url := "https://" + rs[0]
	ac, ok := rtc.dockercfg.DockercfgContents[rs[0]]
	if ok { // if missing, anonymous auth
		hc.Transport = registry.WrapTransport(http.DefaultTransport, url, ac.Username, ac.Password)
	}
	// reg.Ping() fails for quay.io, so we manually construct a registry client here
	reg := registry.Registry{
		URL:    url,
		Client: hc,
		Logf:   rtc.loggerFunc,
	}
	ts, err := reg.Tags(fmt.Sprintf("%v/%v", rs[1], rs[2]))
	if err != nil {
		return false, nil, fmt.Errorf("error getting tags for repo: %v: %v", repo, err)
	}
	lset := set.NewStringSet(tags)
	inter := set.NewStringSet(ts).Intersection(lset)
	return lset.IsEqual(inter), lset.Difference(inter).Items(), nil
}
