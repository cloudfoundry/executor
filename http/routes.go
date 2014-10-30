package http

import "github.com/tedsuo/rata"

const (
	Ping                  = "Ping"
	GetContainer          = "GetContainer"
	AllocateContainer     = "AllocateContainer"
	RunContainer          = "RunContainer"
	DeleteContainer       = "DeleteContainer"
	ListContainers        = "ListContainers"
	GetRemainingResources = "GetRemainingResources"
	GetTotalResources     = "GetTotalResources"
	GetFiles              = "GetFiles"
)

var Routes = rata.Routes{
	{Path: "/ping", Method: "GET", Name: Ping},
	{Path: "/containers", Method: "GET", Name: ListContainers},
	{Path: "/containers", Method: "POST", Name: AllocateContainer},
	{Path: "/containers/:guid", Method: "GET", Name: GetContainer},
	{Path: "/containers/:guid/run", Method: "POST", Name: RunContainer},
	{Path: "/containers/:guid/files", Method: "GET", Name: GetFiles},
	{Path: "/containers/:guid", Method: "DELETE", Name: DeleteContainer},
	{Path: "/resources/remaining", Method: "GET", Name: GetRemainingResources},
	{Path: "/resources/total", Method: "GET", Name: GetTotalResources},
}
