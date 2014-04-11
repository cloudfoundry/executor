package windows_plugin_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestWindowsPlugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "WindowsPlugin Suite")
}
