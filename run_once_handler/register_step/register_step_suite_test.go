package register_step_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRegisterStep(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RegisterStep Suite")
}
