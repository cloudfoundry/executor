package depot_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestDepot(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Depot Suite")
}
