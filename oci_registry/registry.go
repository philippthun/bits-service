package oci_registry

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"

	"github.com/cloudfoundry-incubator/bits-service/oci_registry/blobondemand"
	"github.com/gorilla/mux"
)

//Deprected will be repalced with a real blobstore / dropletstore
type DropletStore interface {
	Get(guid string) *BlobRef
	Set(guid string, blob BlobRef)
}

type BlobRef struct {
	Digest string
	Size   int64
}

type BlobStore interface {
	Put(buf io.Reader) (digest string, size int64, err error)
	PutWithID(guid string, buf io.Reader) (digest string, size int64, err error)
	Has(digest string) bool
	Get(digest string, dest io.Writer) error
}

type BlobHandler struct {
	blobs BlobStore
}

//go:generate counterfeiter . ImageManager
type ImageManager interface {
	GetManifest(string, string) ([]byte, error)
	GetLayer(string, string) (*bytes.Buffer, error)
	Has(digest string) bool
}

type ImageHandler struct {
	imageManager ImageManager
	DropletStore DropletStore
	Rootfs       BlobRef
	BlobStore    BlobStore
}

//go:generate conterfeiter . APIVersionManager
type APIVersionManager interface {
	APIVersion(string, string) ([]byte, error)
}
type APIVersionHandler struct {
	apiVersionManager APIVersionManager
}

func NewImageHandler(imageManager ImageManager) http.Handler {
	mux := mux.NewRouter()
	dropletStore := InMemoryDropletStore{}

	rootfs := BlobRef{
		Digest: "bla",
		Size:   1,
	}
	blobstore := blobondemand.NewInMemoryStore()
	imageHandler := ImageHandler{imageManager, dropletStore, rootfs, blobstore}
	mux.Path("/v2/{name:[a-z0-9/\\.\\-_]+}/manifest/{tag}").Methods(http.MethodGet).HandlerFunc(imageHandler.ServeManifest)
	mux.Path("/v2/{name:[a-z0-9/\\.\\-_]+}/blobs/{digest}").Methods(http.MethodGet).HandlerFunc(imageHandler.ServeLayer)
	return mux
}

func NewAPIVersionHandler(apiVersionManager APIVersionManager) http.Handler {
	mux := mux.NewRouter()
	apiVersionHandler := APIVersionHandler{apiVersionManager}
	mux.Path("/v2/").Methods(http.MethodGet).HandlerFunc(apiVersionHandler.APIVersion)
	return mux
}

func (a APIVersionHandler) APIVersion(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (m ImageHandler) ServeManifest(w http.ResponseWriter, r *http.Request) {
	// tag := mux.Vars(r)["tag"]
	// name := mux.Vars(r)["name"]
	// w.Header().Add("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")

	// manifest, err := m.imageManager.GetManifest(name, tag)
	// if err != nil {
	// 	w.WriteHeader(http.StatusInternalServerError)
	// 	w.Write([]byte("could not receive manifest"))
	// }

	// w.Write(manifest)
	dropletGuid := mux.Vars(r)["guid"]
	droplet := m.DropletStore.Get(dropletGuid)

	config, err := json.Marshal(map[string]interface{}{
		"config": map[string]interface{}{
			"user": "vcap",
		},
		"rootfs": map[string]interface{}{
			"type": "layers",
			"diff_ids": []string{
				m.Rootfs.Digest,
				droplet.Digest,
			},
		},
	})

	if err != nil {
		http.Error(w, "couldnt create config json for droplet", 500)
		return
	}

	configDigest, configSize, err := m.BlobStore.Put(bytes.NewReader(config))
	if err != nil {
		http.Error(w, "couldnt store config json for droplet", 500)
		return
	}

	w.Header().Add("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")

	// nolint
	json.NewEncoder(w).Encode(map[string]interface{}{
		"mediaType":     "application/vnd.docker.distribution.manifest.v2+json",
		"schemaVersion": 2,
		"config": map[string]interface{}{
			"mediaType": "application/vnd.docker.container.image.v1+json",
			"digest":    configDigest,
			"size":      configSize,
		},
		"layers": []map[string]interface{}{
			{
				"digest":    m.Rootfs.Digest,
				"size":      m.Rootfs.Size,
				"mediaType": "application/vnd.docker.image.rootfs.diff.tar",
			},
			{
				"digest":    droplet.Digest,
				"mediaType": "application/vnd.docker.image.rootfs.diff.tar.gzip",
				"size":      droplet.Size,
			},
		},
	})
}

func (m ImageHandler) ServeLayer(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]
	digest := mux.Vars(r)["digest"]

	if ok := m.imageManager.Has(digest); !ok {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte("requested layer not found"))
		return
	}

	layer, err := m.imageManager.GetLayer(name, digest)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("could not receive layer"))
		return
	}

	w.WriteHeader(http.StatusOK)
	_, err = io.Copy(w, layer)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("failed to stream layer"))
	}
}

// InMemoryDropletStore exists because CC droplets aren't content-addressed
// but instead have guids. Therefore we need to store a lookup of droplet digest
// to droplet guid
type InMemoryDropletStore map[string]BlobRef

func (s InMemoryDropletStore) Get(guid string) *BlobRef {
	if r, ok := s[guid]; ok {
		return &r
	}

	return nil
}

func (s InMemoryDropletStore) Set(guid string, blob BlobRef) {
	s[guid] = blob
}
