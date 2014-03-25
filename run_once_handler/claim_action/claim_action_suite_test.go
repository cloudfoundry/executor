package claim_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestClaim_action(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ClaimAction Suite")
}
