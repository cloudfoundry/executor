package limit_container_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestLimitContainerStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LimitContainerStep Suite")
}
