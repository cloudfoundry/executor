package containerstore_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/pivotal-golang/lager/lagertest"

	"testing"
)

var logger *lagertest.TestLogger

func TestContainerstore(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Containerstore Suite")
}

var _ = BeforeEach(func() {
	logger = lagertest.NewTestLogger("test")
})
