package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/cloudfoundry-incubator/runtime-schema/models"
	"github.com/tedsuo/router"
	"io/ioutil"
	"net/http"
)

type Client interface {
	AllocateContainer(ContainerRequest) (ContainerResponse, error)
	InitializeContainer(allocationGuid string) error
	Run(allocationGuid string, request RunRequest) error
	DeleteContainer(allocationGuid string) error
}

type ContainerRequest struct {
	Stack      string
	MemoryMB   int
	DiskMB     int
	CpuPercent float64
	LogConfig  models.LogConfig
}

type RunRequest struct {
	Actions       []models.ExecutorAction
	Metadata      []byte
	CompletionURL string
}

type ContainerResponse struct {
	ContainerRequest
	ExecutorGuid string
	Guid         string
}

func New(httpClient *http.Client, baseUrl string) Client {
	return &client{
		httpClient: httpClient,
		reqGen:     router.NewRequestGenerator(baseUrl, api.Routes),
	}
}

type client struct {
	reqGen     *router.RequestGenerator
	httpClient *http.Client
}

func (c client) AllocateContainer(request ContainerRequest) (ContainerResponse, error) {
	containerResponse := ContainerResponse{}

	response, err := c.makeRequest(api.AllocateContainer, router.Params{}, api.ContainerAllocationRequest{
		MemoryMB:   request.MemoryMB,
		DiskMB:     request.DiskMB,
		CpuPercent: request.CpuPercent,
		Log:        request.LogConfig,
	})

	if response != nil && response.StatusCode == http.StatusServiceUnavailable {
		return containerResponse, fmt.Errorf("Resources %v unavailable", request)
	}

	if err != nil {
		return containerResponse, err
	}

	responseBody, err := ioutil.ReadAll(response.Body)
	if err != nil {
		return containerResponse, err
	}

	response.Body.Close()

	containerJson := api.Container{}
	err = json.Unmarshal(responseBody, &containerJson)
	if err != nil {
		return containerResponse, err
	}

	return ContainerResponse{
		ContainerRequest: request,
		Guid:             containerJson.Guid,
		ExecutorGuid:     containerJson.ExecutorGuid,
	}, nil
}

func (c client) InitializeContainer(allocationGuid string) error {
	_, err := c.makeRequest(api.InitializeContainer, router.Params{"guid": allocationGuid}, nil)
	return err
}

func (c client) Run(allocationGuid string, request RunRequest) error {
	_, err := c.makeRequest(api.RunActions, router.Params{"guid": allocationGuid}, &api.ContainerRunRequest{
		Actions:     request.Actions,
		Metadata:    []byte(request.Metadata),
		CompleteURL: request.CompletionURL,
	})

	return err
}

func (c client) DeleteContainer(allocationGuid string) error {
	_, err := c.makeRequest(api.DeleteContainer, router.Params{"guid": allocationGuid}, nil)
	return err
}

func (c client) makeRequest(handlerName string, params router.Params, payload interface{}) (*http.Response, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := c.reqGen.RequestForHandler(handlerName, params, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if response.StatusCode >= 300 {
		return response, fmt.Errorf("Request failed with status: %d", response.StatusCode)
	}

	return response, nil
}
