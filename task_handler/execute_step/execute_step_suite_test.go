package execute_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestExecuteStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ExecuteStep Suite")
}
