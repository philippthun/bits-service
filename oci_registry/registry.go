package oci_registry

import (
	"bytes"
	"io"
	"net/http"

	"github.com/gorilla/mux"
)

//go:generate counterfeiter . ImageManager
type ImageManager interface {
	GetManifest(string, string) ([]byte, error)
	GetLayer(string, string) (*bytes.Buffer, error)
	Has(digest string) bool
}

type ImageHandler struct {
	imageManager ImageManager
}

func NewHandler(imageManager ImageManager) http.Handler {
	mux := mux.NewRouter()
	imageHandler := ImageHandler{imageManager}
	mux.Path("/v2/{name:[a-z0-9/\\.\\-_]+}/manifest/{tag}").Methods(http.MethodGet).HandlerFunc(imageHandler.ServeManifest)
	mux.Path("/v2/{name:[a-z0-9/\\.\\-_]+}/blobs/{digest}").Methods(http.MethodGet).HandlerFunc(imageHandler.ServeLayer)
	return mux
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
