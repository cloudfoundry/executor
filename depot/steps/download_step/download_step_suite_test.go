package download_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestDownloadStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DownloadStep Suite")
}
