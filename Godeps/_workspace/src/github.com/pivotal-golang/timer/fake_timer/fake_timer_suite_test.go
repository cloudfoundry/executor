package fake_timer_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestFakeTimer(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "FakeTimer Suite")
}
