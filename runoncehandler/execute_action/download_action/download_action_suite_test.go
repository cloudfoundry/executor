package download_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestDownload_action(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Download_action Suite")
}
