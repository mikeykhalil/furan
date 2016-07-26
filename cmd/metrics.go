package cmd

import (
	"fmt"

	"github.com/DataDog/datadog-go/statsd"
)

const (
	namespace = "furan."
)

// MetricsCollector describes an object capabale of pushing metrics somewhere
type MetricsCollector interface {
	Duration(string, string, string, []string, float64) error
	ImageSize(int64, int64, string, string) error
	BuildStarted(string, string) error
	BuildFailed(string, string) error
	BuildSucceeded(string, string) error
}

// DatadogCollector represents a collector that pushes metrics to Datadog
type DatadogCollector struct {
	c *statsd.Client
}

// NewDatadogCollector returns a DatadogCollector using dogstatsd at addr
func NewDatadogCollector(addr string) (*DatadogCollector, error) {
	c, err := statsd.New(addr)
	if err != nil {
		return nil, err
	}
	c.Namespace = namespace
	return &DatadogCollector{
		c: c,
	}, nil
}

func (dc *DatadogCollector) tags(repo, ref string) []string {
	return []string{
		fmt.Sprintf("repo:%v", repo),
		fmt.Sprintf("ref:%v", ref),
	}
}

// Duration pushes duration d (seconds) to the metric name to dogstatsd
func (dc *DatadogCollector) Duration(name string, repo string, ref string, tags []string, d float64) error {
	return dc.c.Histogram(name, d, append(dc.tags(repo, ref), tags...), 1)
}

// ImageSize pushes sz (total size in bytes) and vxz (virtual size in bytes) to dogstatsd
func (dc *DatadogCollector) ImageSize(sz int64, vsz int64, repo string, ref string) error {
	tags := dc.tags(repo, ref)
	err := dc.c.Histogram("image.size_bytes", float64(sz), tags, 1)
	if err != nil {
		return err
	}
	return dc.c.Histogram("image.vsize_bytes", float64(vsz), tags, 1)
}

// BuildStarted increments the counter for each requested build
func (dc *DatadogCollector) BuildStarted(repo, ref string) error {
	return dc.c.Count("build.started", 1, dc.tags(repo, ref), 1)
}

// BuildFailed increments the counter for each build that fails
func (dc *DatadogCollector) BuildFailed(repo, ref string) error {
	return dc.c.Count("build.failed", 1, dc.tags(repo, ref), 1)
}

// BuildSucceeded increments the counter for each build that succeeds
func (dc *DatadogCollector) BuildSucceeded(repo, ref string) error {
	return dc.c.Count("build.succeeded", 1, dc.tags(repo, ref), 1)
}
