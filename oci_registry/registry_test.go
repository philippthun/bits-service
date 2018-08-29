package oci_registry_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"

	registry "github.com/cloudfoundry-incubator/bits-service/oci_registry"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Registry", func() {

	Context("when requesting a manifest", func() {
		var (
			fakeServer *httptest.Server
			handler    http.Handler
		)

		BeforeEach(func() {
			handler = registry.NewHandler()
		})

		JustBeforeEach(func() {
			fakeServer = httptest.NewServer(handler)
		})
		Context("for a particular image name", func() {

			It("it should serve the GET image manifest endpoint ", func() {
				url := fmt.Sprintf("%s%s", fakeServer.URL, "/v2/image-name/manifest/image-tag")
				res, err := http.Get(url)
				Expect(err).NotTo(HaveOccurred())
				Expect(res.StatusCode).To(Equal(http.StatusOK))
			})

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

		It("should serve a docker compatible manifest", func() {

		})

	})

})
