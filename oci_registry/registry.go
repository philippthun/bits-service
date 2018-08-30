package oci_registry

import (
	"net/http"

	"github.com/gorilla/mux"
)

//go:generate counterfeiter . Blobstore
type Blobstore interface {
	GetManifest(string, string) ([]byte, error)
}

func NewHandler(blobstore Blobstore) http.Handler {
	mux := mux.NewRouter()
	manifestHandler := ManifestHandler{blobstore}
	mux.Path("/v2/{name:[a-z0-9/\\.\\-_]+}/manifest/{tag}").Methods(http.MethodGet).Handler(manifestHandler)
	return mux
}

type ManifestHandler struct {
	Blobstore Blobstore
}

func (m ManifestHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	tag := mux.Vars(r)["tag"]
	name := mux.Vars(r)["name"]
	w.Header().Add("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")

	manifest, err := m.Blobstore.GetManifest(name, tag)
	if err != nil {
		w.Write([]byte("could not receive manifest"))
	}

	w.Write(manifest)
}
