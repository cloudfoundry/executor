package checks_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestChecks(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Checks Suite")
}
