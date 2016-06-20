package cmd

import (
	"archive/tar"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path"
	"strings"

	"golang.org/x/net/context"
)

const (
	scratchRoot = "scratch"
	outputRoot  = "output"
)

// Models representing image metadata schemas

// DockerImageManifest represents manifest.json in the root of an image archive
type DockerImageManifest struct {
	Config   string
	RepoTags []string
	Layers   []string
}

// DockerImageConfig represents the image config json file in the root of an
// image archive
type DockerImageConfig struct {
	Architecture    string                 `json:"architecture"`
	Config          map[string]interface{} `json:"config"`
	Container       string                 `json:"string"`
	ContainerConfig map[string]interface{} `json:"container_config"`
	Created         string                 `json:"created"`
	DockerVersion   string                 `json:"docker_version"`
	History         []map[string]string    `json:"history"`
	OS              string                 `json:"os"`
	RootFS          struct {
		Type    string   `json:"type"`
		DiffIDs []string `json:"diff_ids"`
	} `json:"rootfs"`
}

// DockerLayerJSON represents the 'json' file within a layer in an image archive
type DockerLayerJSON struct {
	ID              string                 `json:"id"`
	Parent          string                 `json:"parent,omitempty"`
	Created         string                 `json:"created"`
	ContainerConfig map[string]interface{} `json:"container_config"`
}

// squashedImage allows callers to clean up the temporary file by calling Close()
type squashedImage struct {
	f io.ReadCloser
	n string
}

func (si *squashedImage) Read(p []byte) (int, error) {
	return si.f.Read(p)
}

func (si *squashedImage) Close() error {
	si.f.Close()
	return os.Remove(si.n)
}

// ImageSquasher represents an object capable of squashing a container image
type ImageSquasher interface {
}

// DockerImageSquasher squashes an image repository to its last layer
type DockerImageSquasher struct {
}

// Unpacks the initial input image to the scratch root
func (dis *DockerImageSquasher) unpackInput(input io.Reader, root string) error {
	return nil
}

// Squash processes the input (Docker image tar stream), squashes the image
// to its last layer and returns the tar stream of the squashed image
// Caller is responsible for closing the returned stream when finished to
// release resources
func (dis *DockerImageSquasher) Squash(ctx context.Context, input io.Reader) (io.ReadCloser, error) {
	if isCancelled(ctx.Done()) {
		return nil, fmt.Errorf("squash was cancelled")
	}
	imap, err := dis.unpackToMap(input)
	if err != nil {
		return nil, fmt.Errorf("error unpacking input: %v", err)
	}

	mentry, ok := imap["manifest.json"]
	if !ok {
		return nil, fmt.Errorf("manifest.json not found in image archive")
	}
	manifest := DockerImageManifest{}

	err = json.Unmarshal(mentry.Contents, &manifest)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling image manifest: %v", err)
	}

	if len(manifest.Layers) == 1 {
		return nil, fmt.Errorf("no need to squash: image has one layer")
	}

	// Iterate through layers, unpacking and processing whiteouts
	omap := make(map[string]*tarEntry)
	for _, l := range manifest.Layers {
		_, ok = imap[l]
		if !ok {
			return nil, fmt.Errorf("layer directory not found in image archive: %v", l)
		}
		ltar, ok := imap[fmt.Sprintf("%v/layer.tar", l)]
		if !ok {
			return nil, fmt.Errorf("layer tar not found in image archive: %v", l)
		}
		err = dis.processLayer(ltar.Contents, omap)
		if err != nil {
			return nil, fmt.Errorf("error processing layer: %v: %v", l, err)
		}
	}

	// at this point omap represents a tar file of the final squashed layer
	return dis.constructImage(omap, imap, &manifest)
}

// constructImage takes a map representing the final squashed layer filesystem
// and the metadata from the source image and uses them to contruct a new image
// archive with only the squashed layer
func (dis *DockerImageSquasher) constructImage(layer map[string]*tarEntry, inmap map[string]*tarEntry, manifest *DockerImageManifest) (io.ReadCloser, error) {
	return nil, nil
}

// processLayer takes the input layer, unpacks to outputmap and processes any
// whiteouts present
func (dis *DockerImageSquasher) processLayer(layertar []byte, outputmap map[string]*tarEntry) error {
	var err error
	var ok bool
	var h *tar.Header
	var e *tarEntry
	var contents []byte
	var bn, wt string
	r := tar.NewReader(bytes.NewBuffer(layertar))
	for {
		h, err = r.Next()
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("error getting next entry in layer tar: %v", err)
		}
		bn = path.Base(h.Name)
		if strings.HasPrefix(bn, ".wh.") {
			wt = strings.Replace(h.Name, ".wh.", "", 0)
			if _, ok = outputmap[wt]; !ok {
				return fmt.Errorf("target not found for whiteout: %v", h.Name)
			}
			delete(outputmap, wt)
			continue
		}
		if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			contents, err = ioutil.ReadAll(r)
			if err != nil {
				return fmt.Errorf("error reading tar entry contents: %v", err)
			}
			if int64(len(contents)) != h.Size {
				return fmt.Errorf("tar entry size mismatch: %v: read %v (size: %v)", h.Name, len(contents), h.Size)
			}
		} else {
			contents = []byte{}
		}
		e = &tarEntry{
			Header:   h,
			Contents: contents,
		}
		outputmap[h.Name] = e
	}
}

type tarEntry struct {
	Header   *tar.Header
	Contents []byte
}

// Deserialize a tar stream into a map of filepath -> content
func (dis *DockerImageSquasher) unpackToMap(in io.Reader) (map[string]*tarEntry, error) {
	var err error
	var h *tar.Header
	var e *tarEntry
	var contents []byte
	out := make(map[string]*tarEntry)
	r := tar.NewReader(in)
	for {
		h, err = r.Next()
		if err != nil {
			if err == io.EOF {
				return out, nil
			}
			return out, fmt.Errorf("error getting next tar entry: %v", err)
		}
		if path.IsAbs(h.Name) {
			return out, fmt.Errorf("tar contains absolute path: %v", h.Name)
		}
		if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			contents, err = ioutil.ReadAll(r)
			if err != nil {
				return out, fmt.Errorf("error reading tar entry contents: %v", err)
			}
			if int64(len(contents)) != h.Size {
				return out, fmt.Errorf("tar entry size mismatch: %v: read %v (size: %v)", h.Name, len(contents), h.Size)
			}
		} else {
			contents = []byte{}
		}
		e = &tarEntry{
			Header:   h,
			Contents: contents,
		}
		out[h.Name] = e
	}
}
