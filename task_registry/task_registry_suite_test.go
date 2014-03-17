package task_registry_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestTask_registry(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Task Registry Suite")
}
