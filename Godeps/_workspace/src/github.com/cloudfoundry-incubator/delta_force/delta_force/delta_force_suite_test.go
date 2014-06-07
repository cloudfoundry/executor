package delta_force_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestDeltaForce(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "DeltaForce Suite")
}
