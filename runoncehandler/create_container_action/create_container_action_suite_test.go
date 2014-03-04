package create_container_action_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestCreate_container_action(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "CreateContainerAction Suite")
}
