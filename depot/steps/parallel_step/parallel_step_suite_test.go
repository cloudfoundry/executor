package parallel_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestParallelStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ParallelStep Suite")
}
