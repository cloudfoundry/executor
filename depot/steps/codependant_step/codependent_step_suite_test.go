package codependant_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestCodependentStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CodependentStep Suite")
}
