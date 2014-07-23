package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/cloudfoundry-incubator/executor/api"
	"github.com/tedsuo/rata"
)

func New(httpClient *http.Client, baseUrl string) api.Client {
	return &client{
		httpClient: httpClient,
		reqGen:     rata.NewRequestGenerator(baseUrl, api.Routes),
	}
}

type client struct {
	reqGen     *rata.RequestGenerator
	httpClient *http.Client
}

func (c client) AllocateContainer(allocationGuid string, request api.ContainerAllocationRequest) (api.Container, error) {
	container := api.Container{}

	response, err := c.makeRequest(api.AllocateContainer, rata.Params{"guid": allocationGuid}, request)
	if err != nil {
		return container, err
	}

	defer response.Body.Close()

	if response.StatusCode == http.StatusServiceUnavailable {
		return container, fmt.Errorf("Resources %v unavailable", request)
	}

	err = json.NewDecoder(response.Body).Decode(&container)
	if err != nil {
		return container, err
	}

	return container, nil
}

func (c client) GetContainer(allocationGuid string) (api.Container, error) {
	response, err := c.makeRequest(api.GetContainer, rata.Params{"guid": allocationGuid}, nil)
	if err != nil {
		return api.Container{}, err
	}

	defer response.Body.Close()

	return c.buildContainerFromApiResponse(response)
}

func (c client) InitializeContainer(allocationGuid string, request api.ContainerInitializationRequest) (api.Container, error) {
	response, err := c.makeRequest(api.InitializeContainer, rata.Params{"guid": allocationGuid}, request)
	if err != nil {
		// do some logging
		return api.Container{}, err
	}

	defer response.Body.Close()

	return c.buildContainerFromApiResponse(response)
}

func (c client) Run(allocationGuid string, request api.ContainerRunRequest) error {
	_, err := c.makeRequest(api.RunActions, rata.Params{"guid": allocationGuid}, request)

	return err
}

func (c client) DeleteContainer(allocationGuid string) error {
	_, err := c.makeRequest(api.DeleteContainer, rata.Params{"guid": allocationGuid}, nil)
	return err
}

func (c client) ListContainers() ([]api.Container, error) {
	containers := []api.Container{}

	response, err := c.makeRequest(api.ListContainers, nil, nil)
	if err != nil {
		return containers, err
	}

	defer response.Body.Close()

	err = json.NewDecoder(response.Body).Decode(&containers)
	if err != nil {
		return containers, err
	}

	return containers, nil
}

func (c client) RemainingResources() (api.ExecutorResources, error) {
	return c.getResources(api.GetRemainingResources)
}

func (c client) TotalResources() (api.ExecutorResources, error) {
	return c.getResources(api.GetTotalResources)
}

func (c client) Ping() error {
	response, err := c.makeRequest(api.Ping, nil, nil)
	if err != nil {
		return err
	}

	response.Body.Close()

	return nil
}

func (c client) buildContainerFromApiResponse(response *http.Response) (api.Container, error) {
	container := api.Container{}

	err := json.NewDecoder(response.Body).Decode(&container)
	if err != nil {
		return api.Container{}, err
	}

	return container, nil
}

func (c client) getResources(apiEndpoint string) (api.ExecutorResources, error) {
	resources := api.ExecutorResources{}

	response, err := c.makeRequest(apiEndpoint, nil, nil)
	if err != nil {
		return resources, err
	}

	defer response.Body.Close()

	err = json.NewDecoder(response.Body).Decode(&resources)
	if err != nil {
		return resources, err
	}

	return resources, nil
}

func (c client) makeRequest(handlerName string, params rata.Params, payload interface{}) (*http.Response, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := c.reqGen.CreateRequest(handlerName, params, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if response.StatusCode >= 300 {
		response.Body.Close()

		executorError := response.Header.Get("X-Executor-Error")
		if len(executorError) > 0 {
			err, found := api.Errors[executorError]
			if !found {
				return nil, fmt.Errorf("Unrecognized X-Executor-Error value: %s", executorError)
			}

			return nil, err
		}

		return nil, fmt.Errorf("Request failed with status: %d", response.StatusCode)
	}

	return response, nil
}
