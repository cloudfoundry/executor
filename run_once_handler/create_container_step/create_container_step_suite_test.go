package create_container_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestCreateContainerStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CreateContainerStep Suite")
}
