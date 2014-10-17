package emit_progress_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestEmitProgressStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "EmitProgressStep Suite")
}
