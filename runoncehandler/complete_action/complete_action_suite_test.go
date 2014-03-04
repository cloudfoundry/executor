package complete_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestCompleteAction(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CompleteAction Suite")
}
