package oci_registry

import "net/http"

func (m ImageHandler) APIVersion(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}
