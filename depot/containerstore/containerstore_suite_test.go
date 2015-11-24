package containerstore_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestContainerstore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Containerstore Suite")
}
