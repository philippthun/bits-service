package oci_registry

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cloudfoundry-incubator/bits-service/oci_registry/models/docker"
	"github.com/cloudfoundry-incubator/bits-service/oci_registry/models/docker/mediatype"

	"github.com/gorilla/mux"
)

var digestToLayerMap = make(map[string]string)

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
	// Droplet
	//1. load droplet from filesystem
	//2. prefix the droplet archive with /home/vcap
	//3. generate the sha256 of prefixed droplet archive
	//4. get the size of the prefixed archive
	ociDropletName, err := preFixDroplet("example_droplet")
	// defer os.Remove(ociDropletName)
	if err != nil {
		fmt.Printf("preFixDroplet Error: %v\n", err)
		return nil, err
	}
	dropletDigest := getSHA256(ociDropletName)
	dropletSize, err := getFileSize(ociDropletName)
	if err != nil {
		fmt.Printf("getFileSize Error: %v\n", err)
		return nil, err
	}

	// Rootfs
	// 1. laden aus dem filesystem
	// 2. generate the sha256 of rootfs tar
	// 3. get the size of the rootfs archive
	rootfs, err := os.Open("assets/eirinifs.tar")
	if err != nil {
		fmt.Printf("Error open rootfs: %v\n", err)
		return nil, err
	}
	rootfsDigest := getSHA256(rootfs.Name())

	rootfsSize, err := getFileSize(rootfs.Name())
	if err != nil {
		fmt.Printf("rootfsSize Error: %v\n", err)
		return nil, err
	}

	// Config
	configDigest, configSize, err := getConfigMetaData(rootfsDigest, dropletDigest)
	if err != nil {
		fmt.Printf("Config Metadata Error: %v\n", err)
		return nil, err
	}

	//store digest->filename for later retrieval
	digestToLayerMap[strings.Replace(dropletDigest, "sha256:", "", -1)] = ociDropletName
	digestToLayerMap[strings.Replace(rootfsDigest, "sha256:", "", -1)] = rootfs.Name()

	for k, v := range digestToLayerMap {
		fmt.Println(k, v)
	}

	layers := []docker.Content{
		docker.Content{
			//Rootfs
			MediaType: mediatype.ImageRootfsTarGzip,
			Digest:    rootfsDigest,
			Size:      rootfsSize,
		},
		docker.Content{
			//Droplet
			MediaType: mediatype.ImageRootfsTar,
			Digest:    dropletDigest,
			Size:      dropletSize,
		},
	}

	config := docker.Content{
		MediaType: mediatype.ContainerImageJson,
		Digest:    configDigest,
		Size:      configSize,
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

func (b BitsImageManager) GetLayer(name string, digest string) (*bytes.Buffer, error) {
	fmt.Printf("GetLayer for name %v with digest: %v", name, digest)
	fileName := digestToLayerMap[digest]

	data, err := ioutil.ReadFile(fileName)
	if err != nil {
		fmt.Printf("Read file : %v\n", err)
		return nil, err
	}
	defer os.Remove(fileName)
	return bytes.NewBuffer(data), nil
}

func (b BitsImageManager) Has(digest string) bool {
	fileName := digestToLayerMap[digest]

	fmt.Printf("Has, Filenme is ? %v\n", fileName)
	// layerBits := os.TempDir() + "/" + digest
	if _, err := os.Stat(fileName); os.IsNotExist(err) {
		fmt.Printf("Has, file exist Error: %v\n", err)
		return false
	}

	return true
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
	return fmt.Sprintf("sha256:%x", h.Sum(nil))
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

func getConfigMetaData(rootfsDigest string, dropletDigest string) (string, int64, error) {
	// TODO: ns, tzip why is this necessary?
	// TDOO: ns, tzip how to handle this?
	config, err := json.Marshal(map[string]interface{}{
		"config": map[string]interface{}{
			"user": "vcap",
		},
		"rootfs": map[string]interface{}{
			"type": "layers",
			"diff_ids": []string{
				rootfsDigest,
				dropletDigest,
			},
		},
	})
	buf := bytes.NewReader(config)
	sum := sha256.New()
	stored := &bytes.Buffer{}
	var size int64
	if size, err = io.Copy(io.MultiWriter(sum, stored), buf); err != nil {
		return "", 0, err
	}

	digest := "sha256:" + hex.EncodeToString(sum.Sum(nil))

	return digest, size, nil
}
