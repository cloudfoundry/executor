package tallyman_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestTallyman(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Tallyman Suite")
}
