package allocations_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestAllocations(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Allocations Suite")
}
