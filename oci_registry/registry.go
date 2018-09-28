package oci_registry

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cloudfoundry-incubator/bits-service/oci_registry/models/docker"
	"github.com/cloudfoundry-incubator/bits-service/oci_registry/models/docker/mediatype"

	"github.com/gorilla/mux"
)

//go:generate counterfeiter . ImageManager
type ImageManager interface {
	GetManifest(string, string) ([]byte, error)
	GetLayer(string, string) (*bytes.Buffer, error)
	Has(digest string) bool
}

type ImageHandler struct {
	imageManager ImageManager //ggf *ImageManager (need to adjust the functions accordingly)
}

type APIVersionHandler struct {
}

func NewImageHandler(imageManager ImageManager) http.Handler {
	mux := mux.NewRouter()
	imageHandler := ImageHandler{imageManager}
	mux.Path("/v2/{name:[a-z0-9/\\.\\-_]+}/manifest/{tag}").Methods(http.MethodGet).HandlerFunc(imageHandler.ServeManifest)
	mux.Path("/v2/{name:[a-z0-9/\\.\\-_]+}/blobs/{digest}").Methods(http.MethodGet).HandlerFunc(imageHandler.ServeLayer)
	return mux
}

func NewAPIVersionHandler() http.Handler {
	mux := mux.NewRouter()
	mux.Path("/v2/").Methods(http.MethodGet).HandlerFunc(APIVersion)
	return mux
}

//APIVersion returns HTTP 200 purpously
func APIVersion(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (m ImageHandler) ServeManifest(w http.ResponseWriter, r *http.Request) {
	tag := mux.Vars(r)["tag"]
	name := mux.Vars(r)["name"]
	w.Header().Add("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")

	manifest, err := m.imageManager.GetManifest(name, tag)
	if err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte("could not receive manifest"))
	}

	w.Write(manifest)
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

type BitsImageManager struct {
}

func (b BitsImageManager) GetManifest(string, string) ([]byte, error) {

	dropletSHA := getSHA256("example_droplet")
	fmt.Println(dropletSHA)

	layers := []docker.Content{
		docker.Content{
			MediaType: mediatype.ImageRootfsTarGzip,
			Digest:    "to be calculated",
			Size:      42,
		},
	}

	config := docker.Content{
		MediaType: mediatype.ContainerImageJson,
		Digest:    "to be calculated",
		Size:      1337,
	}

	manifest := docker.Manifest{
		MediaType:     mediatype.DistributionManifestJson,
		SchemaVersion: "v2",
		Config:        config,
		Layers:        layers,
	}

	json, err := json.Marshal(manifest)
	if err != nil {
		return nil, err
	}

	return json, nil
}

func (b BitsImageManager) GetLayer(string, string) (*bytes.Buffer, error) {
	return nil, errors.New("unimplemented")
}

func (b BitsImageManager) Has(digest string) bool {
	return false
}

func getSHA256(fileName string) string {
	f, err := os.Open("assets/" + fileName)
	if err != nil {
		fmt.Printf("%v", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		fmt.Printf("%v", err)
	}
	return fmt.Sprintf("SHA256:%x", h.Sum(nil))
}
