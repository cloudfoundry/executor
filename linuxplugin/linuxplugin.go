package linuxplugin

import (
	"fmt"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

type LinuxPlugin struct{}

func New() *LinuxPlugin {
	return &LinuxPlugin{}
}

func (p LinuxPlugin) BuildRunScript(run models.RunAction) string {
	script := ""

	for _, envPair := range run.Env {
		// naively assumes Go's string quotes are compatible with Bash,
		// which it's not. See http://golang.org/ref/spec#String_literals
		if len(envPair) == 2 {
			script += fmt.Sprintf("export %s=%q\n", envPair[0], envPair[1])
		}
	}

	script += run.Script

	return script
}

func (p LinuxPlugin) BuildCreateDirectoryRecursivelyCommand(path string) string {
	return fmt.Sprintf("mkdir -p %s", path)
}
