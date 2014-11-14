/*
Executor component tests require garden-linux to be running, and can be found
in github.com/cloudfoundry-incubator/inigo
*/
package main_test

import (
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

func TestExecutor(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Executor Suite")
}
