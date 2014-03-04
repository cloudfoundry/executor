package action_runner_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestAction_runner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Action_runner Suite")
}
