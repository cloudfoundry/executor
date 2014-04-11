package windows_plugin_test

import (
	. "github.com/cloudfoundry-incubator/executor/windows_plugin"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("WindowsPlugin", func() {
	var plugin *WindowsPlugin

	BeforeEach(func() {
		plugin = New()
	})

	Describe("BuildRunScript", func() {
		It("returns the script prepended by exported environment variables", func() {
			Ω(plugin.BuildRunScript(models.RunAction{
				Script: "shutdown -r",
				Env: [][]string{
					{"FOO", `1 "foo" bar`},
					{"BAR", "2"},
				},
			})).Should(Equal(`$env:FOO="1 \"foo\" bar"
$env:BAR="2"
shutdown -r`))
		})

		Context("when the environment variables are messed up", func() {
			It("ignores the messed up env variables", func() {
				Ω(plugin.BuildRunScript(models.RunAction{
					Script: "shutdown -r",
					Env: [][]string{
						{"FOO", "1"},
						{},
						{"BAZ"},
						{"BANANA", "TOO", "LONG"},
						{"BAR", "2"},
					},
				})).Should(Equal(`$env:FOO="1"
$env:BAR="2"
shutdown -r`))
			})
		})
	})
})
