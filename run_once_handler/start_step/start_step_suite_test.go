package start_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestStartStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "StartStep Suite")
}
