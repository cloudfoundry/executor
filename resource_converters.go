package executor

import "code.cloudfoundry.org/bbs/models"

func EnvironmentVariablesToModel(envVars []EnvironmentVariable) []models.EnvironmentVariable {
	out := make([]models.EnvironmentVariable, len(envVars))
	for i, val := range envVars {
		out[i].Name = val.Name
		out[i].Value = val.Value
	}
	return out
}

func EnvironmentVariablesFromModel(envVars []*models.EnvironmentVariable) []EnvironmentVariable {
	out := make([]EnvironmentVariable, len(envVars))
	for i, val := range envVars {
		out[i].Name = val.Name
		out[i].Value = val.Value
	}
	return out
}

func FilesBasedServiceBindingFromModel(envFiles []*models.Files) []ServiceBindingFiles {
	out := make([]ServiceBindingFiles, len(envFiles))
	for i, envFile := range envFiles {
		out[i].Name = envFile.Name
		out[i].Value = envFile.Value
	}

	return out
}
