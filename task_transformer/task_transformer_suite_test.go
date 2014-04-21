package task_transformer_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestTaskTransformer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "TaskTransformer Suite")
}
