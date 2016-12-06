package routes

import (
	"archive/zip"
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"io/ioutil"

	"path/filepath"

	"github.com/gorilla/mux"
	"github.com/pkg/errors"
)

type NotFoundError struct {
	error
}

func NewNotFoundError() *NotFoundError {
	return &NotFoundError{fmt.Errorf("NotFoundError")}
}

type Blobstore interface {
	Get(path string) (body io.ReadCloser, redirectLocation string, err error)
	Put(path string, src io.ReadSeeker) (redirectLocation string, err error)
	Exists(path string) (bool, error)
	Delete(path string) error
}

func SetUpAppStashRoutes(router *mux.Router, blobstore Blobstore) {
	handler := &AppStashHandler{blobstore: blobstore}
	router.Path("/app_stash/entries").Methods("POST").HandlerFunc(handler.PostEntries)
	router.Path("/app_stash/matches").Methods("POST").HandlerFunc(handler.PostMatches)
	router.Path("/app_stash/bundles").Methods("POST").HandlerFunc(handler.PostBundles)
}

func SetUpPackageRoutes(router *mux.Router, blobstore Blobstore) {
	handler := &ResourceHandler{blobstore: blobstore, resourceType: "package"}
	router.Path("/packages/{guid}").Methods("PUT").HandlerFunc(handler.Put)
	router.Path("/packages/{guid}").Methods("GET").HandlerFunc(handler.Get)
	router.Path("/packages/{guid}").Methods("DELETE").HandlerFunc(handler.Delete)
}

func SetUpBuildpackRoutes(router *mux.Router, blobstore Blobstore) {
	handler := &ResourceHandler{blobstore: blobstore, resourceType: "buildpack"}
	router.Path("/buildpacks/{guid}").Methods("PUT").HandlerFunc(handler.Put)
	// TODO change Put/Get/etc. signature to allow this:
	// router.Path("/buildpacks/{guid}").Methods("PUT").HandlerFunc(delegateTo(handler.Put))
	router.Path("/buildpacks/{guid}").Methods("GET").HandlerFunc(handler.Get)
	router.Path("/buildpacks/{guid}").Methods("DELETE").HandlerFunc(handler.Delete)
}

func delegateTo(delegate func(http.ResponseWriter, *http.Request, map[string]string)) func(http.ResponseWriter, *http.Request) {
	return func(responseWriter http.ResponseWriter, request *http.Request) {
		delegate(responseWriter, request, mux.Vars(request))
	}
}

func SetUpDropletRoutes(router *mux.Router, blobstore Blobstore) {
	handler := &ResourceHandler{blobstore: blobstore, resourceType: "droplet"}
	router.Path("/droplets/{guid}").Methods("PUT").HandlerFunc(handler.Put)
	router.Path("/droplets/{guid}").Methods("GET").HandlerFunc(handler.Get)
	router.Path("/droplets/{guid}").Methods("DELETE").HandlerFunc(handler.Delete)
}

func SetUpBuildpackCacheRoutes(router *mux.Router, blobstore Blobstore) {
	handler := &BuildpackCacheHandler{blobStore: blobstore}
	router.Path("/buildpack_cache/entries/{app_guid}/{stack_name}").Methods("PUT").HandlerFunc(handler.Put)
	router.Path("/buildpack_cache/entries/{app_guid}/{stack_name}").Methods("GET").HandlerFunc(handler.Get)
	router.Path("/buildpack_cache/entries/{app_guid}/{stack_name}").Methods("DELETE").HandlerFunc(handler.Delete)
	router.Path("/buildpack_cache/entries/{app_guid}/").Methods("DELETE").HandlerFunc(handler.DeleteAppGuid)
	router.Path("/buildpack_cache/entries").Methods("DELETE").HandlerFunc(handler.DeleteEntries)
}

type AppStashHandler struct {
	blobstore Blobstore
}

func (handler *AppStashHandler) PostEntries(responseWriter http.ResponseWriter, request *http.Request) {
	uploadedFile, _, e := request.FormFile("application")
	if e != nil {
		badRequest(responseWriter, "Could not retrieve 'application' form parameter")
		return
	}
	defer uploadedFile.Close()

	tempZipFile, e := ioutil.TempFile("", "")
	if e != nil {
		internalServerError(responseWriter, e)
		return
	}
	defer os.Remove(tempZipFile.Name())
	defer tempZipFile.Close()

	_, e = io.Copy(tempZipFile, uploadedFile)
	if e != nil {
		internalServerError(responseWriter, e)
		return
	}

	openZipFile, e := zip.OpenReader(tempZipFile.Name())
	if e != nil {
		badRequest(responseWriter, "Bad Request: Not a valid zip file")
		return
	}
	defer openZipFile.Close()

	for _, zipFileEntry := range openZipFile.File {
		if !zipFileEntry.FileInfo().Mode().IsRegular() {
			continue
		}
		e = copyTo(handler.blobstore, zipFileEntry)
		if e != nil {
			internalServerError(responseWriter, e)
			return
		}
	}
}

func copyTo(blobstore Blobstore, zipFileEntry *zip.File) error {
	unzippedReader, e := zipFileEntry.Open()
	if e != nil {
		return errors.WithStack(e)
	}
	defer unzippedReader.Close()

	tempZipEntryFile, e := ioutil.TempFile("", filepath.Base(zipFileEntry.Name))
	if e != nil {
		return errors.WithStack(e)
	}
	defer os.Remove(tempZipEntryFile.Name())
	defer tempZipEntryFile.Close()

	sha, e := copyCalculatingSha(tempZipEntryFile, unzippedReader)
	if e != nil {
		return errors.WithStack(e)
	}

	entryFileRead, e := os.Open(tempZipEntryFile.Name())
	if e != nil {
		return errors.WithStack(e)
	}
	defer entryFileRead.Close()

	// TODO: this assumes no redirect on PUTs. Is that always true?
	_, e = blobstore.Put(sha, entryFileRead)
	if e != nil {
		return errors.WithStack(e)
	}

	return nil
}

func copyCalculatingSha(writer io.Writer, reader io.Reader) (sha string, e error) {
	checkSum := sha1.New()
	multiWriter := io.MultiWriter(writer, checkSum)

	_, e = io.Copy(multiWriter, reader)
	if e != nil {
		return "", fmt.Errorf("error copying. Caused by: %v", e)
	}

	return fmt.Sprintf("%x", checkSum.Sum(nil)), nil
}

func (handler *AppStashHandler) PostMatches(responseWriter http.ResponseWriter, request *http.Request) {
	body, e := ioutil.ReadAll(request.Body)
	if e != nil {
		internalServerError(responseWriter, e)
		return
	}
	var sha1s []map[string]string
	e = json.Unmarshal(body, &sha1s)
	if e != nil {
		log.Printf("Invalid body %s", body)
		responseWriter.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprintf(responseWriter, "Invalid body %s", body)
		return
	}
	var responseSha1 []map[string]string
	for _, entry := range sha1s {
		exists, e := handler.blobstore.Exists(entry["sha1"])
		if e != nil {
			internalServerError(responseWriter, e)
			return
		}
		if !exists {
			responseSha1 = append(responseSha1, map[string]string{"sha1": entry["sha1"]})
		}
	}
	response, e := json.Marshal(&responseSha1)
	if e != nil {
		internalServerError(responseWriter, e)
		return
	}
	fmt.Fprintf(responseWriter, "%s", response)
}

type BundlesPayload struct {
	Sha1 string
	Fn   string
	Mode os.FileMode
}

func (handler *AppStashHandler) PostBundles(responseWriter http.ResponseWriter, request *http.Request) {
	body, e := ioutil.ReadAll(request.Body)
	if e != nil {
		internalServerError(responseWriter, e)
		return
	}

	var bundlesPayload []BundlesPayload
	e = json.Unmarshal(body, &bundlesPayload)
	if e != nil {
		log.Printf("Invalid body %s", body)
		responseWriter.WriteHeader(http.StatusUnprocessableEntity)
		fmt.Fprintf(responseWriter, "Invalid body %s", body)
		return
	}

	tempZipFilename, e := handler.createTempZipFileFrom(bundlesPayload)
	if e != nil {
		internalServerError(responseWriter, e)
		return
	}
	defer os.Remove(tempZipFilename)

	tempZipFile, e := os.Open(tempZipFilename)
	if e != nil {
		internalServerError(responseWriter, e)
		return
	}
	defer tempZipFile.Close()

	_, e = io.Copy(responseWriter, tempZipFile)
	if e != nil {
		internalServerError(responseWriter, e)
		return
	}
}

func (handler *AppStashHandler) createTempZipFileFrom(bundlesPayload []BundlesPayload) (tempFilename string, err error) {
	tempFile, e := ioutil.TempFile("", "bundles")
	if e != nil {
		return "", e
	}
	defer tempFile.Close()
	zipWriter := zip.NewWriter(tempFile)
	for _, entry := range bundlesPayload {
		zipEntry, e := zipWriter.CreateHeader(zipEntryHeader(entry.Fn, entry.Mode))
		if e != nil {
			return "", e
		}

		// TODO this assumes no redirects. Probably app_stash should have its own interface for blobstore that expresses no redirects
		b, _, e := handler.blobstore.Get(entry.Sha1)
		if e != nil {
			return "", e
		}
		defer b.Close()

		_, e = io.Copy(zipEntry, b)
		if e != nil {
			return "", e
		}
	}
	zipWriter.Close()
	return tempFile.Name(), nil
}

func zipEntryHeader(name string, mode os.FileMode) *zip.FileHeader {
	header := &zip.FileHeader{
		Name:   name,
		Method: zip.Deflate,
	}
	header.SetMode(mode)
	return header
}

type ResourceHandler struct {
	blobstore    Blobstore
	resourceType string
}

func (handler *ResourceHandler) Put(responseWriter http.ResponseWriter, request *http.Request) {
	file, _, e := request.FormFile(handler.resourceType)
	if e != nil {
		badRequest(responseWriter, "Could not retrieve '%s' form parameter", handler.resourceType)
		return
	}
	defer file.Close()

	redirectLocation, e := handler.blobstore.Put(pathFor(mux.Vars(request)["guid"]), file)

	if e != nil {
		internalServerError(responseWriter, e)
		return
	}

	if redirectLocation != "" {
		redirect(responseWriter, redirectLocation)
		return
	}

	responseWriter.WriteHeader(http.StatusCreated)
}

func (handler *ResourceHandler) Get(responseWriter http.ResponseWriter, request *http.Request) {
	body, redirectLocation, e := handler.blobstore.Get(pathFor(mux.Vars(request)["guid"]))
	switch e.(type) {
	case *NotFoundError:
		responseWriter.WriteHeader(http.StatusNotFound)
		return
	case error:
		internalServerError(responseWriter, e)
		return
	}
	if redirectLocation != "" {
		redirect(responseWriter, redirectLocation)
		return
	}
	defer body.Close()
	responseWriter.WriteHeader(http.StatusOK)
	io.Copy(responseWriter, body)
}

func (handler *ResourceHandler) Delete(responseWriter http.ResponseWriter, request *http.Request) {
	// TODO
}

func pathFor(identifier string) string {
	return fmt.Sprintf("/%s/%s/%s", identifier[0:2], identifier[2:4], identifier)
}

type BuildpackCacheHandler struct {
	blobStore Blobstore
}

func (handler *BuildpackCacheHandler) Put(responseWriter http.ResponseWriter, request *http.Request) {
	file, _, e := request.FormFile("buildpack_cache")
	if e != nil {
		badRequest(responseWriter, "Could not retrieve buildpack_cache form parameter")
		return
	}
	defer file.Close()

	redirectLocation, e := handler.blobStore.Put(
		fmt.Sprintf("/buildpack_cache/entries/%s/%s", mux.Vars(request)["app_guid"], mux.Vars(request)["stack_name"]), file)

	if e != nil {
		internalServerError(responseWriter, e)
		return
	}

	if redirectLocation != "" {
		redirect(responseWriter, redirectLocation)
		return
	}
	responseWriter.WriteHeader(http.StatusCreated)
}

func (handler *BuildpackCacheHandler) Get(responseWriter http.ResponseWriter, request *http.Request) {
	body, redirectLocation, e := handler.blobStore.Get(
		fmt.Sprintf("/buildpack_cache/entries/%s/%s", mux.Vars(request)["app_guid"], mux.Vars(request)["stack_name"]))
	switch e.(type) {
	case NotFoundError:
		responseWriter.WriteHeader(http.StatusNotFound)
		return
	case error:
		internalServerError(responseWriter, e)
		return
	}
	if redirectLocation != "" {
		redirect(responseWriter, redirectLocation)
		return
	}
	defer body.Close()
	responseWriter.WriteHeader(http.StatusOK)
	io.Copy(responseWriter, body)
}

func (handler *BuildpackCacheHandler) Delete(responseWriter http.ResponseWriter, request *http.Request) {
	e := handler.blobStore.Delete(
		fmt.Sprintf("/buildpack_cache/entries/%s/%s", mux.Vars(request)["app_guid"], mux.Vars(request)["stack_name"]))
	writeResponseBasedOnError(responseWriter, e)
}

func (handler *BuildpackCacheHandler) DeleteAppGuid(responseWriter http.ResponseWriter, request *http.Request) {
	e := handler.blobStore.Delete(
		fmt.Sprintf("/buildpack_cache/entries/%s", mux.Vars(request)["app_guid"]))
	writeResponseBasedOnError(responseWriter, e)
}

func (handler *BuildpackCacheHandler) DeleteEntries(responseWriter http.ResponseWriter, request *http.Request) {
	e := handler.blobStore.Delete("/buildpack_cache/entries")
	writeResponseBasedOnError(responseWriter, e)
}

func writeResponseBasedOnError(responseWriter http.ResponseWriter, e error) {
	switch e.(type) {
	case NotFoundError:
		responseWriter.WriteHeader(http.StatusNotFound)
		return
	case error:
		internalServerError(responseWriter, e)
		return
	}
	responseWriter.WriteHeader(http.StatusOK)
}

func redirect(responseWriter http.ResponseWriter, redirectLocation string) {
	responseWriter.WriteHeader(http.StatusFound)
	responseWriter.Header().Set("Location", redirectLocation)
}

func internalServerError(responseWriter http.ResponseWriter, e error) {
	log.Printf("%+v", e)
	responseWriter.WriteHeader(http.StatusInternalServerError)
}

func badRequest(responseWriter http.ResponseWriter, message string, args ...interface{}) {
	responseWriter.WriteHeader(http.StatusBadRequest)
	fmt.Fprintf(responseWriter, message, args...)
}