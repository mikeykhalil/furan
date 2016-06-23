package cmd

import (
	"archive/tar"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"path"
	"strings"

	"golang.org/x/net/context"
)

const (
	// needed for correct file modes for tar archive entries
	regFileMode = 0100644
	dirMode     = 040755
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
	Architecture    string                   `json:"architecture"`
	Config          map[string]interface{}   `json:"config"`
	Container       string                   `json:"container"`
	ContainerConfig map[string]interface{}   `json:"container_config"`
	Created         string                   `json:"created"`
	DockerVersion   string                   `json:"docker_version"`
	History         []map[string]interface{} `json:"history"`
	OS              string                   `json:"os"`
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
	Container       string                 `json:"container"`
	ContainerConfig map[string]interface{} `json:"container_config"`
	DockerVersion   string                 `json:"docker_version"`
	Config          map[string]interface{} `json:"config"`
	Architecture    string                 `json:"architecture"`
	OS              string                 `json:"os"`
}

// WhiteoutFile represents a file that was removed from final squashed image
// via whiteout
type WhiteoutFile struct {
	Name string // Name of target removed file
	Size uint64 // Size in bytes
}

// SquashInfo represents data about an individual squashing operation
type SquashInfo struct {
	InputBytes        uint64
	OutputBytes       uint64
	SizeDifference    int64
	FilesRemovedCount uint
	FilesRemoved      []WhiteoutFile
	LayersRemoved     uint
}

// ImageSquasher represents an object capable of squashing a container image
type ImageSquasher interface {
	Squash(context.Context, io.Reader, io.Writer) (*SquashInfo, error)
}

// DockerImageSquasher squashes an image repository to its last layer
type DockerImageSquasher struct {
	logger *log.Logger
}

// NewDockerImageSquasher returns a Docker Image Squasher using the specified logger
func NewDockerImageSquasher(logger *log.Logger) ImageSquasher {
	return &DockerImageSquasher{
		logger: logger,
	}
}

func (dis *DockerImageSquasher) logf(msg string, params ...interface{}) {
	dis.logger.Printf(msg+"\n", params...)
}

// Squash processes the input (Docker image tar stream), squashes the image
// to its last layer and returns the tar stream of the squashed image
func (dis *DockerImageSquasher) Squash(ctx context.Context, input io.Reader, output io.Writer) (*SquashInfo, error) {
	if isCancelled(ctx.Done()) {
		return nil, fmt.Errorf("squash was cancelled")
	}
	dis.logf("squashing image")

	sinfo := &SquashInfo{}

	imap, insz, err := dis.unpackToMap(ctx, input)
	if err != nil {
		return nil, fmt.Errorf("error unpacking input: %v", err)
	}
	sinfo.InputBytes = uint64(insz)

	mentry, ok := imap["manifest.json"]
	if !ok {
		return nil, fmt.Errorf("manifest.json not found in image archive")
	}
	mraw := []DockerImageManifest{}
	manifest := DockerImageManifest{}

	err = json.Unmarshal(mentry.Contents, &mraw)
	if err != nil {
		return nil, fmt.Errorf("error unmarshaling image manifest: %v", err)
	}
	if len(mraw) != 1 {
		return nil, fmt.Errorf("unexpected image manifest array length (expected 1): %v", len(mraw))
	}
	manifest = mraw[0]

	if len(manifest.Layers) < 2 {
		return nil, fmt.Errorf("no need to squash: image has one layer")
	}

	rmlayers := []string{}
	for _, l := range manifest.Layers[0 : len(manifest.Layers)-1] {
		rmlayers = append(rmlayers, strings.Replace(l, "/layer.tar", "", 1))
	}

	sinfo.LayersRemoved = uint(len(rmlayers))

	// Iterate through layers, unpacking and processing whiteouts
	flc, wl, err := dis.processLayers(ctx, imap, &manifest)
	if err != nil {
		return nil, fmt.Errorf("error processing layers: %v", err)
	}

	sinfo.FilesRemoved = wl

	// Get a map of final layer tar entries suitable for merging/replacing into imap
	flmap, err := dis.constructFinalLayer(ctx, &manifest, imap, flc)
	if err != nil {
		return nil, fmt.Errorf("error serializing final layer: %v", err)
	}

	// Adjust metadata for final image
	mdmap, err := dis.adjustMetadata(ctx, &manifest, imap, flmap)
	if err != nil {
		return nil, fmt.Errorf("error adjusting image metadata: %v", err)
	}

	// Merge all tar entry maps and serialize into tar stream
	sz, err := dis.finalImage(ctx, imap, flmap, mdmap, rmlayers, output)
	if err != nil {
		return nil, fmt.Errorf("error finalizing image: %v", err)
	}

	sinfo.OutputBytes = uint64(sz)
	sinfo.SizeDifference = int64(sinfo.OutputBytes - sinfo.InputBytes)
	sinfo.FilesRemovedCount = uint(len(sinfo.FilesRemoved))

	dis.logf("squashing complete")

	return sinfo, nil
}

// finalLayerID returns the final layer ID from a given manifest
func (dis *DockerImageSquasher) finalLayerID(manifest *DockerImageManifest) string {
	return strings.Replace(manifest.Layers[len(manifest.Layers)-1], "/layer.tar", "", 1)
}

// finalImage takes the original input map and merges in the maps produced in
// previous steps, removes squashed layers and then serializes the result into a tar stream
// written to output. Returns the count of bytes written (not including tar metadata)
func (dis *DockerImageSquasher) finalImage(ctx context.Context, imap map[string]*tarEntry, flmap map[string]*tarEntry, mdmap map[string]*tarEntry, rmlayers []string, output io.Writer) (int64, error) {
	if isCancelled(ctx.Done()) {
		return 0, fmt.Errorf("squash was cancelled")
	}
	dis.logf("inserting final squashed layer and removing unneeded layers")
	for k, v := range flmap {
		if _, ok := imap[k]; !ok {
			return 0, fmt.Errorf("file from processed final layer missing from input map: %v", k)
		}
		imap[k] = v
	}
	for k, v := range mdmap {
		if _, ok := imap[k]; !ok {
			return 0, fmt.Errorf("file from image metadata missing from input map: %v", k)
		}
		imap[k] = v
	}
	var jn, vn, tn, ldir, l, v string
	var ok bool
	for _, l = range rmlayers {
		jn = fmt.Sprintf("%v/json", l)
		vn = fmt.Sprintf("%v/VERSION", l)
		tn = fmt.Sprintf("%v/layer.tar", l)
		ldir = fmt.Sprintf("%v/", l)
		for _, v = range []string{jn, vn, tn, ldir} {
			if _, ok = imap[v]; !ok {
				return 0, fmt.Errorf("removing layer: file missing from input map: %v", v)
			}
			delete(imap, v)
		}
	}
	sz, err := dis.serializeTarEntries(imap, output)
	if err != nil {
		return 0, fmt.Errorf("error serializing final image: %v", err)
	}
	return sz, nil
}

// adjustMetadata changes the image metadata to agree with the final single layer:
// Keep final layer id
// Calculate new layer diff_id
// remove all other diff_ids from top-level config
// remove all other layers from manifest.json
func (dis *DockerImageSquasher) adjustMetadata(ctx context.Context, manifest *DockerImageManifest, imap map[string]*tarEntry, flmap map[string]*tarEntry) (map[string]*tarEntry, error) {
	if isCancelled(ctx.Done()) {
		return nil, fmt.Errorf("squash was cancelled")
	}
	dis.logf("adjusting metadata to be consistent with squashed layers")
	out := make(map[string]*tarEntry)
	flid := dis.finalLayerID(manifest)
	fln := fmt.Sprintf("%v/layer.tar", flid)
	if _, ok := flmap[fln]; !ok {
		return out, fmt.Errorf("final layer tar not found after serializing: %v", flid)
	}
	sum := sha256.Sum256(flmap[fln].Contents)
	diffid := fmt.Sprintf("sha256:%v", hex.EncodeToString(sum[:]))

	config := DockerImageConfig{}
	cent, ok := imap[manifest.Config]
	if !ok {
		return out, fmt.Errorf("top level config not found in image archive: %v", manifest.Config)
	}
	err := json.Unmarshal(cent.Contents, &config)
	if err != nil {
		return out, fmt.Errorf("error deserializing top level config: %v", err)
	}
	config.RootFS.DiffIDs = []string{diffid}
	config.History = []map[string]interface{}{config.History[len(config.History)-1]}
	manifest.Layers = []string{fmt.Sprintf("%v/layer.tar", flid)}
	cb, err := json.Marshal(&config)
	if err != nil {
		return out, fmt.Errorf("error marshaling image config: %v", err)
	}
	marr := []DockerImageManifest{*manifest}
	mb, err := json.Marshal(&marr)
	if err != nil {
		return out, fmt.Errorf("error marshaling image manifest: %v", err)
	}
	out[manifest.Config] = &tarEntry{
		Header: &tar.Header{
			Name:     manifest.Config,
			Typeflag: tar.TypeReg,
			Mode:     regFileMode,
		},
		Contents: cb,
	}
	out["manifest.json"] = &tarEntry{
		Header: &tar.Header{
			Name:     "manifest.json",
			Typeflag: tar.TypeReg,
			Mode:     regFileMode,
		},
		Contents: mb,
	}
	return out, nil
}

// constructFinalLayer takes the final layer contents ("{layer-id}/layer.tar"),
// adjusts metadata, serializes and returns a map suitable for merging/replacing
// into the original input
func (dis *DockerImageSquasher) constructFinalLayer(ctx context.Context, manifest *DockerImageManifest, imap map[string]*tarEntry, flc map[string]*tarEntry) (map[string]*tarEntry, error) {
	if isCancelled(ctx.Done()) {
		return nil, fmt.Errorf("squash was cancelled")
	}
	dis.logf("constructing metadata for final layer")
	flmd := DockerLayerJSON{}
	flid := dis.finalLayerID(manifest)
	flmdent, ok := imap[fmt.Sprintf("%v/json", flid)]
	if !ok {
		return nil, fmt.Errorf("final layer json not found in image archive: %v", flid)
	}
	err := json.Unmarshal(flmdent.Contents, &flmd)
	if err != nil {
		return nil, fmt.Errorf("error deserializing final layer json: %v", err)
	}
	vn := fmt.Sprintf("%v/VERSION", flid)
	vent, ok := imap[vn]
	if !ok {
		return nil, fmt.Errorf("final layer VERSION not found in image archive: %v", flid)
	}
	return dis.serializeFinalLayer(ctx, flc, &flmd, vent.Contents)
}

// serializeFinalLayer takes the layer map, metadata and version and serializes
// into the final layer.tar, json and VERSION tar entries
func (dis *DockerImageSquasher) serializeFinalLayer(ctx context.Context, layer map[string]*tarEntry, md *DockerLayerJSON, version []byte) (map[string]*tarEntry, error) {
	out := make(map[string]*tarEntry)
	if isCancelled(ctx.Done()) {
		return nil, fmt.Errorf("squash was cancelled")
	}
	dis.logf("creating final squashed layer filesystem image")
	ltb := bytes.NewBuffer([]byte{})
	_, err := dis.serializeTarEntries(layer, ltb)
	if err != nil {
		return nil, fmt.Errorf("error serializing layer: %v", err)
	}
	layertar := ltb.Bytes()
	md.Parent = ""
	mdn := fmt.Sprintf("%v/", md.ID)
	out[mdn] = &tarEntry{
		Header: &tar.Header{
			Name:     mdn,
			Typeflag: tar.TypeDir,
			Mode:     dirMode,
		},
		Contents: []byte{},
	}
	mdb, err := json.Marshal(&md)
	if err != nil {
		return out, fmt.Errorf("error marshaling layer json: %v", err)
	}
	jn := fmt.Sprintf("%v/json", md.ID)
	out[jn] = &tarEntry{
		Header: &tar.Header{
			Name:     jn,
			Typeflag: tar.TypeReg,
			Size:     int64(len(mdb)),
			Mode:     regFileMode,
		},
		Contents: mdb,
	}
	vn := fmt.Sprintf("%v/VERSION", md.ID)
	out[vn] = &tarEntry{
		Header: &tar.Header{
			Name:     vn,
			Typeflag: tar.TypeReg,
			Size:     int64(len(version)),
			Mode:     regFileMode,
		},
		Contents: version,
	}
	tn := fmt.Sprintf("%v/layer.tar", md.ID)
	out[tn] = &tarEntry{
		Header: &tar.Header{
			Name:     tn,
			Typeflag: tar.TypeReg,
			Size:     int64(len(layertar)),
			Mode:     regFileMode,
		},
		Contents: layertar,
	}
	return out, nil
}

// serializeTarEntries produces a tar stream from a map of entries
// returns the total bytes written (not including tar metadata)
func (dis *DockerImageSquasher) serializeTarEntries(input map[string]*tarEntry, output io.Writer) (int64, error) {
	var tsz int64
	var i int
	w := tar.NewWriter(output)
	defer w.Close()
	dis.logf("serializing tar entries")
	for k, v := range input {
		v.Header.Size = int64(len(v.Contents))
		err := w.WriteHeader(v.Header)
		if err != nil {
			return tsz, fmt.Errorf("error writing layer tar header: %v: %v", k, err)
		}
		i, err = w.Write(v.Contents)
		if err != nil {
			return tsz, fmt.Errorf("error writing layer tar content: %v: %v", k, err)
		}
		tsz += int64(i)
	}
	w.Flush()
	return tsz, nil
}

// processLayers iterates through the image layers in order, processing any whiteouts
// present. Returns a map of tar entries suitable to be serialized into "{layer-id}/layer.tar"
// and a list of files removed by whiteout during squashing
func (dis *DockerImageSquasher) processLayers(ctx context.Context, imap map[string]*tarEntry, manifest *DockerImageManifest) (map[string]*tarEntry, []WhiteoutFile, error) {
	var err error
	var ok bool
	var ldir string
	var lwl, wl []WhiteoutFile
	omap := make(map[string]*tarEntry)
	for _, l := range manifest.Layers {
		if isCancelled(ctx.Done()) {
			return nil, wl, fmt.Errorf("squash was cancelled")
		}
		if !strings.HasSuffix(l, "/layer.tar") {
			return nil, wl, fmt.Errorf("unexpected format for layer entry (expected '.../layer.tar'): %v", l)
		}
		ldir = strings.Replace(l, "layer.tar", "", 1)
		_, ok = imap[ldir]
		if !ok {
			return nil, wl, fmt.Errorf("layer directory not found in image archive: %v", ldir)
		}
		ltar, ok := imap[l]
		if !ok {
			return nil, wl, fmt.Errorf("layer tar not found in image archive: %v", l)
		}
		dis.logf("processing layer %v", strings.Replace(l, "/layer.tar", "", 1))
		lwl, err = dis.processLayer(ctx, ltar.Contents, omap)
		if err != nil {
			return nil, wl, fmt.Errorf("error processing layer: %v: %v", l, err)
		}
		wl = append(wl, lwl...)
	}
	return omap, wl, nil
}

// processLayer takes the input layer, unpacks to outputmap and processes any
// whiteouts present
func (dis *DockerImageSquasher) processLayer(ctx context.Context, layertar []byte, outputmap map[string]*tarEntry) ([]WhiteoutFile, error) {
	var err error
	var ok bool
	var h *tar.Header
	var e *tarEntry
	var contents []byte
	var bn, wt string
	wl := []WhiteoutFile{}
	r := tar.NewReader(bytes.NewBuffer(layertar))
	for {
		if isCancelled(ctx.Done()) {
			return wl, fmt.Errorf("squash was cancelled")
		}
		h, err = r.Next()
		if err != nil {
			if err == io.EOF {
				return wl, nil
			}
			return wl, fmt.Errorf("error getting next entry in layer tar: %v", err)
		}
		bn = path.Base(h.Name)
		if strings.HasPrefix(bn, ".wh.") {
			wt = strings.Replace(h.Name, ".wh.", "", 2) // I've seen "double whiteouts" in the wild
			if _, ok = outputmap[wt]; !ok {
				wt = fmt.Sprintf("%v/", wt) // directory whiteouts need a slash appended
				if _, ok = outputmap[wt]; !ok {
					dis.logf("warning: target not found for whiteout 2: %v\n", h.Name)
					continue
				}
			}
			wl = append(wl, WhiteoutFile{
				Name: wt,
				Size: uint64(outputmap[wt].Header.Size),
			})
			delete(outputmap, wt)
			continue
		}
		if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			contents, err = ioutil.ReadAll(r)
			if err != nil {
				return wl, fmt.Errorf("error reading tar entry contents: %v", err)
			}
			if int64(len(contents)) != h.Size {
				return wl, fmt.Errorf("tar entry size mismatch: %v: read %v (size: %v)", h.Name, len(contents), h.Size)
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
// returns the map and the total bytes read (not including tar metadata)
func (dis *DockerImageSquasher) unpackToMap(ctx context.Context, in io.Reader) (map[string]*tarEntry, int64, error) {
	var err error
	var h *tar.Header
	var e *tarEntry
	var contents []byte
	var tsz int64
	out := make(map[string]*tarEntry)
	r := tar.NewReader(in)
	dis.logf("unpacking input")
	for {
		if isCancelled(ctx.Done()) {
			return out, tsz, fmt.Errorf("squash was cancelled")
		}
		h, err = r.Next()
		if err != nil {
			if err == io.EOF {
				return out, tsz, nil
			}
			return out, tsz, fmt.Errorf("error getting next tar entry: %v", err)
		}
		if path.IsAbs(h.Name) {
			return out, tsz, fmt.Errorf("tar contains absolute path: %v", h.Name)
		}
		if h.Typeflag == tar.TypeReg || h.Typeflag == tar.TypeRegA {
			contents, err = ioutil.ReadAll(r)
			if err != nil {
				return out, tsz, fmt.Errorf("error reading tar entry contents: %v", err)
			}
			if int64(len(contents)) != h.Size {
				return out, tsz, fmt.Errorf("tar entry size mismatch: %v: read %v (size: %v)", h.Name, len(contents), h.Size)
			}
			tsz += int64(len(contents))
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
