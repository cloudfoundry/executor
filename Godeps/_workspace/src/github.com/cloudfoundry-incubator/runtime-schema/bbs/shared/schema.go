package shared

import (
	"path"
	"strconv"

	"github.com/cloudfoundry-incubator/runtime-schema/models"
)

const SchemaRoot = "/v1/"
const ExecutorSchemaRoot = SchemaRoot + "executor"
const RepSchemaRoot = SchemaRoot + "rep"
const FileServerSchemaRoot = SchemaRoot + "file_server"
const LongRunningProcessSchemaRoot = SchemaRoot + "transitional_lrp"
const LRPStartAuctionSchemaRoot = SchemaRoot + "start"
const TaskSchemaRoot = SchemaRoot + "task"
const LockSchemaRoot = SchemaRoot + "locks"

func ExecutorSchemaPath(executorID string) string {
	return path.Join(ExecutorSchemaRoot, executorID)
}

func RepSchemaPath(repID string) string {
	return path.Join(RepSchemaRoot, repID)
}

func FileServerSchemaPath(segments ...string) string {
	return path.Join(append([]string{FileServerSchemaRoot}, segments...)...)
}

func TransitionalLongRunningProcessSchemaPath(lrp models.TransitionalLongRunningProcess) string {
	return path.Join(LongRunningProcessSchemaRoot, lrp.Guid)
}

func LRPStartAuctionSchemaPath(lrp models.LRPStartAuction) string {
	return path.Join(LRPStartAuctionSchemaRoot, lrp.Guid, strconv.Itoa(lrp.Index))
}

func TaskSchemaPath(task models.Task) string {
	return path.Join(TaskSchemaRoot, task.Guid)
}

func LockSchemaPath(lockName string) string {
	return path.Join(LockSchemaRoot, lockName)
}
