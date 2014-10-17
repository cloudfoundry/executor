package upload_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestUploadStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "UploadStep Suite")
}
