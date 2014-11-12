package timeout_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestTimeoutStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TimeoutStep Suite")
}
