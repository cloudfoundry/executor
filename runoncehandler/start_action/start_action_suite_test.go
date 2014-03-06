package start_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestStart_action(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Start_action Suite")
}
