package shared

import (
	"path"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

const SchemaRoot = "/v1/"
const ExecutorSchemaRoot = SchemaRoot + "executor"
const FileServerSchemaRoot = SchemaRoot + "file_server"
const LongRunningProcessSchemaRoot = SchemaRoot + "transitional_lrp"
const TaskSchemaRoot = SchemaRoot + "task"
const LockSchemaRoot = SchemaRoot + "locks"

func ExecutorSchemaPath(executorID string) string {
	return path.Join(ExecutorSchemaRoot, executorID)
}

func FileServerSchemaPath(segments ...string) string {
	return path.Join(append([]string{FileServerSchemaRoot}, segments...)...)
}

func TransitionalLongRunningProcessSchemaPath(lrp models.TransitionalLongRunningProcess) string {
	return path.Join(LongRunningProcessSchemaRoot, lrp.Guid)
}

func TaskSchemaPath(task models.Task) string {
	return path.Join(TaskSchemaRoot, task.Guid)
}

func LockSchemaPath(lockName string) string {
	return path.Join(LockSchemaRoot, lockName)
}
