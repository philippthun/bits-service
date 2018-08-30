package oci_registry_test

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"

	registry "github.com/cloudfoundry-incubator/bits-service/oci_registry"
	"github.com/cloudfoundry-incubator/bits-service/oci_registry/models/docker"
	"github.com/cloudfoundry-incubator/bits-service/oci_registry/models/docker/mediatype"
	"github.com/cloudfoundry-incubator/bits-service/oci_registry/oci_registryfakes"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Registry", func() {

	Context("when requesting a manifest", func() {
		var (
			fakeServer *httptest.Server
			handler    http.Handler
			url        string
			fakeBlob   *oci_registryfakes.FakeBlobstore
		)

		BeforeEach(func() {
			fakeBlob = new(oci_registryfakes.FakeBlobstore)
			handler = registry.NewHandler(fakeBlob)
			url = "/v2/image-name/manifest/image-tag"
			fakeServer = httptest.NewServer(handler)
		})

		Context("for an image name and tag", func() {
			var (
				res    *http.Response
				err    error
				config docker.Content
				layers []docker.Content
			)

			BeforeEach(func() {
				url = "/v2/image-name/manifest/image-tag"
				config = docker.Content{
					MediaType: mediatype.ContainerImageJson,
					Digest:    "sha256:my-super-digest",
					Size:      1234,
				}

				layers = []docker.Content{
					{
						MediaType: mediatype.ImageRootfsTarGzip,
						Digest:    "sha256:my-layer-one-digest",
						Size:      4321,
					},
				}
			})

			JustBeforeEach(func() {
				url = fmt.Sprintf("%s%s", fakeServer.URL, url)
				res, err = http.Get(url)
			})

			It("should not fail", func() {
				Expect(err).NotTo(HaveOccurred())
			})

			It("should serve the GET image manifest endpoint ", func() {
				Expect(res.StatusCode).To(Equal(http.StatusOK))
			})

			It("should ask for the manifest of desired image and tag", func() {
				name, tag := fakeBlob.GetManifestArgsForCall(0)
				Expect(name).To(Equal("image-name"))
				Expect(tag).To(Equal("image-tag"))
			})

			XIt("should serve a docker compatible manifest", func() {
				body, err := ioutil.ReadAll(res.Body)
				Expect(err).NotTo(HaveOccurred())

				expected := toDockerManifest(config, layers)

				actual := docker.Manifest{}
				err = json.Unmarshal(body, &actual)
				Expect(&actual).To(Equal(expected))
			})
		})

		Context("for image names have multiple paths or special chars", func() {

			It("it should support / in the name path parameter", func() {
				url := fmt.Sprintf("%s%s", fakeServer.URL, "/v2/image/name/manifest/image-tag")
				res, err := http.Get(url)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusOK))
			})

			It("it should support mulitple / in the name path parameter", func() {
				url := fmt.Sprintf("%s%s", fakeServer.URL, "/v2/image/tag/v/22/name/manifest/image-tag")
				res, err := http.Get(url)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusOK))
			})

			It("it NOT should support special characters in the name path parameter", func() {
				url := fmt.Sprintf("%s%s", fakeServer.URL, "/v2/image/tag@/v/!22/name/manifest/image-tag")
				res, err := http.Get(url)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusNotFound))
			})
		})

	})

})

func toDockerManifest(config docker.Content, layers []docker.Content) *docker.Manifest {
	return &docker.Manifest{
		MediaType:     mediatype.DistributionManifestJson,
		SchemaVersion: 2,
		Config:        config,
		Layers:        layers,
	}
}
