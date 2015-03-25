package http

import "github.com/tedsuo/rata"

const (
	Ping                  = "Ping"
	Events                = "Events"
	GetContainer          = "GetContainer"
	AllocateContainers    = "AllocateContainers"
	RunContainer          = "RunContainer"
	StopContainer         = "StopContainer"
	DeleteContainer       = "DeleteContainer"
	ListContainers        = "ListContainers"
	GetRemainingResources = "GetRemainingResources"
	GetTotalResources     = "GetTotalResources"
	GetFiles              = "GetFiles"
	GetMetrics            = "GetMetrics"
	GetAllMetrics         = "GetAllMetrics"
)

var Routes = rata.Routes{
	{Path: "/ping", Method: "GET", Name: Ping},
	{Path: "/events", Method: "GET", Name: Events},
	{Path: "/containers", Method: "GET", Name: ListContainers},
	{Path: "/containers", Method: "POST", Name: AllocateContainers},
	{Path: "/containers/:guid", Method: "GET", Name: GetContainer},
	{Path: "/containers/:guid/run", Method: "POST", Name: RunContainer},
	{Path: "/containers/:guid/files", Method: "GET", Name: GetFiles},
	{Path: "/containers/:guid/metrics", Method: "GET", Name: GetMetrics},
	{Path: "/containers/:guid/stop", Method: "POST", Name: StopContainer},
	{Path: "/containers/:guid", Method: "DELETE", Name: DeleteContainer},
	{Path: "/metrics", Method: "GET", Name: GetAllMetrics},
	{Path: "/resources/remaining", Method: "GET", Name: GetRemainingResources},
	{Path: "/resources/total", Method: "GET", Name: GetTotalResources},
}
