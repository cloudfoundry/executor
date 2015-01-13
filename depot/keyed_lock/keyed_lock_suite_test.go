package keyed_lock_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestKeyedLock(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "KeyedLock Suite")
}
