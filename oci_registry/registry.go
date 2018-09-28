package oci_registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"time"

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

func AddImageHandler(router *mux.Router, imageManager ImageManager) {

	imageHandler := ImageHandler{imageManager}
	router.Path("/v2/{name:[a-z0-9/\\.\\-_]+}/manifest/{tag}").Methods(http.MethodGet).HandlerFunc(imageHandler.ServeManifest)
	router.Path("/v2/{name:[a-z0-9/\\.\\-_]+}/blobs/{digest}").Methods(http.MethodGet).HandlerFunc(imageHandler.ServeLayer)
}

func AddAPIVersionHandler(router *mux.Router) {
	// mux := mux.NewRouter()
	router.Path("/v2").Methods(http.MethodGet).HandlerFunc(APIVersion)
	router.Path("/v2/").Methods(http.MethodGet).HandlerFunc(APIVersion)
	// return mux
}

//APIVersion returns HTTP 200 purpously
func APIVersion(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	fmt.Printf("[%s]\tReceived Ping\n", time.Now().Format(time.RFC3339))
	fmt.Fprintf(w, "Pong")
}

func (m ImageHandler) ServeManifest(w http.ResponseWriter, r *http.Request) {
	tag := mux.Vars(r)["tag"]
	name := mux.Vars(r)["name"]
	w.Header().Add("Content-Type", "application/vnd.docker.distribution.manifest.v2+json")

	manifest, err := m.imageManager.GetManifest(name, tag)
	if err != nil {
		fmt.Printf("GetManifest Error: %v\n", err)
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
	//1. load droplet from filesystem
	//2. prefix the droplet archive with /home/vcap
	//3. generate the sha256 of prefixed droplet archive
	//4. get the size of the prefixed archive
	ociDropletName, err := preFixDroplet("example_droplet")
	defer os.Remove(ociDropletName)
	if err != nil {
		fmt.Printf("preFixDroplet Error: %v\n", err)
		return nil, err
	}
	dropletSHA := getSHA256(ociDropletName)
	fmt.Println(dropletSHA)

	// dropFile, err := os.Open(ociDropletName)
	// if err != nil {
	// 	return nil, err
	// }
	// dropFile.Stat()
	// dropFileInfo, err := dropFile.Stat()
	// if err != nil {
	// 	return nil, err
	// }
	// fmt.Printf("The file is %d bytes long\n", dropFileInfo.Size())

	dropletSize, err := getFileSize(ociDropletName)
	if err != nil {
		fmt.Printf("getFileSize Error: %v\n", err)
		return nil, err
	}

	layers := []docker.Content{
		docker.Content{
			//Rootfs
			MediaType: mediatype.ImageRootfsTarGzip,
			Digest:    "to be calculated",
			Size:      42,
		},
		docker.Content{
			//Droplet
			MediaType: mediatype.ImageRootfsTar,
			Digest:    dropletSHA,
			Size:      dropletSize,
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
		fmt.Printf("Marshal Error: %v\n", err)
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
	fmt.Printf("sha256 convert file: %v\n", fileName)
	f, err := os.Open(fileName)
	if err != nil {
		fmt.Printf("%v\n", err)
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		fmt.Printf("%v\n", err)
	}
	return fmt.Sprintf("sha256:%x\n", h.Sum(nil))
}

func preFixDroplet(fileName string) (string, error) {

	cfDroplet, err := os.Open("assets/" + fileName)
	if err != nil {
		return "", err
	}
	ociDroplet, err := ioutil.TempFile("", "converted-layer")
	if err != nil {
		return "", err
	}

	layer := tar.NewWriter(ociDroplet)

	gz, err := gzip.NewReader(cfDroplet)
	if err != nil {
		return "", err
	}

	t := tar.NewReader(gz)
	for {
		hdr, err := t.Next()
		if err == io.EOF {
			break
		}

		if err != nil {
			return "", err
		}

		hdr.Name = filepath.Join("/home/vcap", hdr.Name)
		layer.WriteHeader(hdr)
		if _, err := io.Copy(layer, t); err != nil {
			return "", err
		}
	}

	return ociDroplet.Name(), nil
}

func getFileSize(fileName string) (int64, error) {
	file, err := os.Open(fileName)
	if err != nil {
		return -1, err
	}

	fileInfo, err := file.Stat()
	if err != nil {
		return -1, err
	}
	fmt.Printf("The file is %d bytes long\n", fileInfo.Size())
	return fileInfo.Size(), nil
}
