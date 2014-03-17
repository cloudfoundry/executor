package linux_plugin_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"testing"
)

func TestLinuxplugin(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Linuxplugin Suite")
}
