package lazy_sequence_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestLazySequence(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LazySequence Suite")
}
