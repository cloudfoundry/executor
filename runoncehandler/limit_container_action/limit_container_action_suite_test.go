package limit_container_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestLimit_container_action(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Limit_container_action Suite")
}
