package transformer

import "code.cloudfoundry.org/bbs/models"

func envoyRunAction(envoyArgs []string) models.RunAction {
	return models.RunAction{
		LogSource: "PROXY",
		Path:      "/etc/cf-assets/envoy/envoy",
		Args:      envoyArgs,
	}
}
