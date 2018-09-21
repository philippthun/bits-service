package cmd

import (
	"fmt"
	"log"
	"net/http"
	"os"

	registry "github.com/cloudfoundry-incubator/bits-service/eirini_registry"
	"github.com/cloudfoundry-incubator/bits-service/eirini_registry/blobondemand"
	"github.com/spf13/cobra"
)

var registryCmd = &cobra.Command{
	Use:   "registry",
	Short: "starts the eirini registry",
	Run:   reg,
}

func reg(cmd *cobra.Command, args []string) {
	blobstore := blobondemand.NewInMemoryStore()

	path, err := cmd.Flags().GetString("rootfs")
	exitWithError(err)

	rootfsTar, err := os.Open(path)
	exitWithError(err)

	rootfsDigest, rootfsSize, err := blobstore.Put(rootfsTar)
	exitWithError(err)

	log.Fatal(http.ListenAndServe("0.0.0.0:8080", registry.NewHandler(
		registry.BlobRef{
			Digest: rootfsDigest,
			Size:   rootfsSize,
		},
		make(registry.InMemoryDropletStore),
		blobstore,
	)))
}

func initRegistry() {
	registryCmd.Flags().StringP("rootfs", "r", "", "Path to the rootfs tarball")
}

func exitWithError(err error) {
	if err != nil {
		fmt.Fprintf(os.Stderr, "Exit: %s", err.Error())
		os.Exit(1)
	}
}
