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
const LRPStartAuctionSchemaRoot = SchemaRoot + "start"
const ActualLRPSchemaRoot = SchemaRoot + "actual"
const DesiredLRPSchemaRoot = SchemaRoot + "desired"
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

func LRPStartAuctionSchemaPath(lrp models.LRPStartAuction) string {
	return path.Join(LRPStartAuctionSchemaRoot, lrp.Guid, strconv.Itoa(lrp.Index))
}

func ActualLRPSchemaPath(lrp models.LRP) string {
	return path.Join(ActualLRPSchemaRoot, lrp.ProcessGuid, strconv.Itoa(lrp.Index), lrp.InstanceGuid)
}

func DesiredLRPSchemaPath(lrp models.DesiredLRP) string {
	return path.Join(DesiredLRPSchemaRoot, lrp.ProcessGuid)
}

func TaskSchemaPath(task models.Task) string {
	return path.Join(TaskSchemaRoot, task.Guid)
}

func LockSchemaPath(lockName string) string {
	return path.Join(LockSchemaRoot, lockName)
}
