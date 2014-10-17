package try_step_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestTryStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TryStep Suite")
}
