package api

import "github.com/tedsuo/router"

const (
	Ping                  = "Ping"
	GetContainer          = "GetContainer"
	AllocateContainer     = "AllocateContainer"
	InitializeContainer   = "InitializeContainer"
	RunActions            = "RunActions"
	DeleteContainer       = "DeleteContainer"
	ListContainers        = "ListContainers"
	GetRemainingResources = "GetRemainingResources"
	GetTotalResources     = "GetTotalResources"
)

var Routes = router.Routes{
	{Path: "/ping", Method: "GET", Handler: Ping},
	{Path: "/containers", Method: "GET", Handler: ListContainers},
	{Path: "/containers/:guid", Method: "GET", Handler: GetContainer},
	{Path: "/containers/:guid", Method: "POST", Handler: AllocateContainer},
	{Path: "/containers/:guid/initialize", Method: "POST", Handler: InitializeContainer},
	{Path: "/containers/:guid/run", Method: "POST", Handler: RunActions},
	{Path: "/containers/:guid", Method: "DELETE", Handler: DeleteContainer},
	{Path: "/resources/remaining", Method: "GET", Handler: GetRemainingResources},
	{Path: "/resources/total", Method: "GET", Handler: GetTotalResources},
}
