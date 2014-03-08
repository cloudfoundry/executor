package upload_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestUpload_action(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Upload_action Suite")
}
