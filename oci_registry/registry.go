package oci_registry

import (
	"net/http"

	"github.com/gorilla/mux"
)

func NewHandler() http.Handler {
	mux := mux.NewRouter()
	mux.Path("/v2/{name:\\w*[a-z0-9/-]*\\w}/manifest/{tag}").Methods(http.MethodGet).HandlerFunc(handleManifest)
	return mux
}

func handleManifest(w http.ResponseWriter, req *http.Request) {
	w.WriteHeader(http.StatusOK)
}
