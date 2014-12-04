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
	"github.com/vito/go-sse/sse"

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

func (c client) AllocateContainer(request executor.Container) (executor.Container, error) {
	container := executor.Container{}

	response, err := c.doRequest(ehttp.AllocateContainer, nil, request, nil)
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
	response, err := c.doRequest(ehttp.GetContainer, rata.Params{"guid": allocationGuid}, nil, nil)
	if err != nil {
		return executor.Container{}, err
	}

	defer response.Body.Close()

	return c.buildContainerFromApiResponse(response)
}

func (c client) RunContainer(allocationGuid string) error {
	response, err := c.doRequest(ehttp.RunContainer, rata.Params{"guid": allocationGuid}, nil, nil)
	if err != nil {
		// do some logging
		return err
	}

	return response.Body.Close()
}

func (c client) DeleteContainer(allocationGuid string) error {
	response, err := c.doRequest(ehttp.DeleteContainer, rata.Params{"guid": allocationGuid}, nil, nil)
	if err != nil {
		// do some logging
		return err
	}

	return response.Body.Close()
}

func (c client) ListContainers(tags executor.Tags) ([]executor.Container, error) {
	containers := []executor.Container{}

	filter := make([]string, 0, len(tags))
	for name, value := range tags {
		filter = append(filter, fmt.Sprintf("%s:%s", name, value))
	}

	response, err := c.doRequest(ehttp.ListContainers, nil, nil, url.Values{"tag": filter})
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

	response, err = handleResponse(response)
	if err != nil {
		return nil, err
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
	response, err := c.doRequest(ehttp.Ping, nil, nil, nil)
	if err != nil {
		return err
	}

	response.Body.Close()

	return nil
}

func (c client) SubscribeToEvents() (<-chan executor.Event, error) {
	events := make(chan executor.Event)

	source := &sse.EventSource{
		Client: c.httpClient,
		CreateRequest: func() *http.Request {
			request, err := c.reqGen.CreateRequest(ehttp.Events, nil, nil)
			if err != nil {
				panic(err) // totally shouldn't happen
			}

			return request
		},
	}

	go func() {
		for {
			sseEvent, err := source.Next()
			if err != nil {
				close(events)
				break
			}

			switch executor.EventType(sseEvent.Name) {
			case executor.EventTypeContainerComplete:
				event := executor.ContainerCompleteEvent{}

				err := json.Unmarshal(sseEvent.Data, &event)
				if err == nil {
					events <- event
				}

			case executor.EventTypeContainerRunning:
				event := executor.ContainerRunningEvent{}

				err := json.Unmarshal(sseEvent.Data, &event)
				if err == nil {
					events <- event
				}
			}
		}
	}()

	return events, nil
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

	response, err := c.doRequest(apiEndpoint, nil, nil, nil)
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

func (c client) doRequest(handlerName string, params rata.Params, payload interface{}, queryParameters url.Values) (*http.Response, error) {
	jsonBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}

	req, err := c.reqGen.CreateRequest(handlerName, params, bytes.NewReader(jsonBody))
	if err != nil {
		return nil, err
	}

	if queryParameters != nil {
		req.URL.RawQuery = queryParameters.Encode()
	}

	req.Header.Set("Content-Type", "application/json")

	response, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	return handleResponse(response)
}

func handleResponse(response *http.Response) (*http.Response, error) {
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
