package linux_plugin_test

import (
	. "github.com/cloudfoundry-incubator/executor/linux_plugin"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("LinuxPlugin", func() {
	var plugin *LinuxPlugin

	BeforeEach(func() {
		plugin = New()
	})

	Describe("BuildRunScript", func() {
		It("returns the script prepended by exported environment variables", func() {
			Ω(plugin.BuildRunScript(models.RunAction{
				Script: "sudo reboot",
				Env: [][]string{
					{"FOO", "1"},
					{"BAR", "2"},
				},
			})).Should(Equal(`export FOO="1"
export BAR="2"
sudo reboot`))
		})

		Context("when the environment variables are messed up", func() {
			It("ignores the messed up env variables", func() {
				Ω(plugin.BuildRunScript(models.RunAction{
					Script: "sudo reboot",
					Env: [][]string{
						{"FOO", "1"},
						{},
						{"BAZ"},
						{"BANANA", "TOO", "LONG"},
						{"BAR", "2"},
					},
				})).Should(Equal(`export FOO="1"
export BAR="2"
sudo reboot`))
			})
		})
	})

	Describe("BuildCreateDirectoryRecursivelyCommand", func() {
		It("creates the directory and its parents", func() {
			Ω(plugin.BuildCreateDirectoryRecursivelyCommand("/some/path")).Should(Equal("mkdir -p /some/path"))
		})
	})
})
