package api

import "github.com/tedsuo/rata"

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

var Routes = rata.Routes{
	{Path: "/ping", Method: "GET", Name: Ping},
	{Path: "/containers", Method: "GET", Name: ListContainers},
	{Path: "/containers/:guid", Method: "GET", Name: GetContainer},
	{Path: "/containers/:guid", Method: "POST", Name: AllocateContainer},
	{Path: "/containers/:guid/initialize", Method: "POST", Name: InitializeContainer},
	{Path: "/containers/:guid/run", Method: "POST", Name: RunActions},
	{Path: "/containers/:guid", Method: "DELETE", Name: DeleteContainer},
	{Path: "/resources/remaining", Method: "GET", Name: GetRemainingResources},
	{Path: "/resources/total", Method: "GET", Name: GetTotalResources},
}
