package sequence_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestSequence(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Sequence Suite")
}
