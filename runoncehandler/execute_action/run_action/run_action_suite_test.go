package run_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRunAction(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RunAction Suite")
}
