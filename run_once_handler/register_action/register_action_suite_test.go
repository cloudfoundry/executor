package register_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestRegister_action(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "RegisterAction Suite")
}
