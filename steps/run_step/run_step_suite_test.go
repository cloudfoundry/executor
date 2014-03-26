package run_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRunStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RunAction Suite")
}
