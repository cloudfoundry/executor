package lazy_action_runner_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestLazyActionRunner(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "LazyActionRunner Suite")
}
