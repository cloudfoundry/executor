package execute_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestExecuteAction(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "ExecuteAction Suite")
}
