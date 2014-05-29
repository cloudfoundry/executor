package fake_bbs_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestFakeBbs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Fake BBS Suite")
}
