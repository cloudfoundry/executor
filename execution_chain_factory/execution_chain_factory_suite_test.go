package execution_chain_factory_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestExecution_chain_factory(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Execution_chain_factory Suite")
}
