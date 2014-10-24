package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/cloudfoundry-incubator/executor"
	ehttp "github.com/cloudfoundry-incubator/executor/http"

	"github.com/tedsuo/rata"
)

func New(httpClient *http.Client, baseUrl string) executor.Client {
	return &client{
		httpClient: httpClient,
		reqGen:     rata.NewRequestGenerator(baseUrl, ehttp.Routes),
	}
}

type client struct {
	reqGen     *rata.RequestGenerator
	httpClient *http.Client
}

func (c client) AllocateContainer(allocationGuid string, request executor.ContainerAllocationRequest) (executor.Container, error) {
	container := executor.Container{}

	response, err := c.makeRequest(ehttp.AllocateContainer, rata.Params{"guid": allocationGuid}, request)
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

func (c client) GetContainer(allocationGuid string) (executor.Container, error) {
	response, err := c.makeRequest(ehttp.GetContainer, rata.Params{"guid": allocationGuid}, nil)
	if err != nil {
		return executor.Container{}, err
	}

	defer response.Body.Close()

	return c.buildContainerFromApiResponse(response)
}

func (c client) InitializeContainer(allocationGuid string) (executor.Container, error) {
	response, err := c.makeRequest(ehttp.InitializeContainer, rata.Params{"guid": allocationGuid}, nil)
	if err != nil {
		// do some logging
		return executor.Container{}, err
	}

	defer response.Body.Close()

	return c.buildContainerFromApiResponse(response)
}

func (c client) Run(allocationGuid string, request executor.ContainerRunRequest) error {
	_, err := c.makeRequest(ehttp.RunActions, rata.Params{"guid": allocationGuid}, request)

	return err
}

func (c client) DeleteContainer(allocationGuid string) error {
	_, err := c.makeRequest(ehttp.DeleteContainer, rata.Params{"guid": allocationGuid}, nil)
	return err
}

func (c client) ListContainers() ([]executor.Container, error) {
	containers := []executor.Container{}

	response, err := c.makeRequest(ehttp.ListContainers, nil, nil)
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

func (c client) GetFiles(guid, sourcePath string) (io.ReadCloser, error) {
	req, err := c.reqGen.CreateRequest(ehttp.GetFiles, rata.Params{"guid": guid}, nil)
	if err != nil {
		return nil, err
	}
	req.URL.RawQuery = url.Values{"source": []string{sourcePath}}.Encode()

	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if response.StatusCode >= 300 {
		response.Body.Close()

		executorError := response.Header.Get("X-Executor-Error")
		if len(executorError) > 0 {
			err, found := executor.Errors[executorError]
			if !found {
				return nil, fmt.Errorf("Unrecognized X-Executor-Error value: %s", executorError)
			}

			return nil, err
		}

		return nil, fmt.Errorf("Request failed with status: %d", response.StatusCode)
	}

	return response.Body, nil
}

func (c client) RemainingResources() (executor.ExecutorResources, error) {
	return c.getResources(ehttp.GetRemainingResources)
}

func (c client) TotalResources() (executor.ExecutorResources, error) {
	return c.getResources(ehttp.GetTotalResources)
}

func (c client) Ping() error {
	response, err := c.makeRequest(ehttp.Ping, nil, nil)
	if err != nil {
		return err
	}

	response.Body.Close()

	return nil
}

func (c client) buildContainerFromApiResponse(response *http.Response) (executor.Container, error) {
	container := executor.Container{}

	err := json.NewDecoder(response.Body).Decode(&container)
	if err != nil {
		return executor.Container{}, err
	}

	return container, nil
}

func (c client) getResources(apiEndpoint string) (executor.ExecutorResources, error) {
	resources := executor.ExecutorResources{}

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
			err, found := executor.Errors[executorError]
			if !found {
				return nil, fmt.Errorf("Unrecognized X-Executor-Error value: %s", executorError)
			}

			return nil, err
		}

		return nil, fmt.Errorf("Request failed with status: %d", response.StatusCode)
	}

	return response, nil
}
