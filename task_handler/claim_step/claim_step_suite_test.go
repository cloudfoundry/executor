package claim_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestClaimStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ClaimStep Suite")
}
