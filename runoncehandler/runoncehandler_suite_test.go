package runoncehandler_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRunoncehandler(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Runoncehandler Suite")
}
