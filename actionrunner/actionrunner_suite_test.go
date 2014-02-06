package actionrunner_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestActionrunner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Actionrunner Suite")
}
