package cmd

import (
	"github.com/docker/engine-api/client"
)

// ImageBuilder represents
type ImageBuilder struct {
	c *client.Client
}

// NewImageBuilder returns a new ImageBuilder
func NewImageBuilder() (*ImageBuilder, error) {
	ib := &ImageBuilder{}
	dc, err := client.NewEnvClient()
	if err != nil {
		return ib, err
	}
	ib.c = dc
	return ib, nil
}

// Build builds and pushes an image accourding to the request
func (ib *ImageBuilder) Build(req *buildRequest) error {
	return nil
}
