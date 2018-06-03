package vcontainer

import (
	"io"

	"code.cloudfoundry.org/lager"

	"github.com/virtualcloudfoundry/vcontainercommon/verrors"

	"github.com/virtualcloudfoundry/vcontainercommon/vcontainermodels"
)

type StreamOutAdapter struct {
	logger          lager.Logger
	client          vcontainermodels.VContainer_StreamOutClient
	currentResponse []byte
	currentIndex    int
}

func NewStreamOutAdapter(logger lager.Logger, client vcontainermodels.VContainer_StreamOutClient) io.ReadCloser {
	return &StreamOutAdapter{
		logger:          logger,
		client:          client,
		currentResponse: nil,
		currentIndex:    0,
	}
}

func (s *StreamOutAdapter) Read(p []byte) (n int, err error) {
	// s.logger.Info("stream-out-adapter-read", lager.Data{"current_idx": s.currentIndex})
	if s.currentResponse == nil || s.currentIndex == len(s.currentResponse) {
		s.logger.Info("stream-out-adapter-recv-more")
		response, err := s.client.Recv()

		if err != nil {
			if err == io.EOF {
				s.logger.Info("stream-out-adapter-read-recv-eof-got")
				return 0, io.EOF
			} else {
				s.logger.Error("stream-out-adapter-read-recv-failed", err)
				return 0, verrors.New("stream-out-adapter-read-failed")
			}
		}
		s.logger.Info("stream-out-adapter-read", lager.Data{
			"buffer_size": len(p),
			"content_len": len(response.Content)})
		s.currentResponse = response.Content
		s.currentIndex = 0
	}

	// todo compress this.
	bufferSize := len(p)
	currentRespSize := len(s.currentResponse)
	if leftSize := currentRespSize - s.currentIndex; leftSize < bufferSize {
		// s.logger.Info("stream-out-adapter-read-left-size-small", lager.Data{
		// 	"left_size":         leftSize,
		// 	"current_idx":       s.currentIndex,
		// 	"current_resp_size": currentRespSize})
		copy(p, s.currentResponse[s.currentIndex:])
		s.currentIndex = currentRespSize
		return leftSize, nil
	} else {
		// s.logger.Info("stream-out-adapter-read-left-size-enough", lager.Data{
		// 	"current_idx":       s.currentIndex,
		// 	"current_resp_size": currentRespSize})
		copy(p, s.currentResponse[s.currentIndex:])
		s.currentIndex += bufferSize
		return bufferSize, nil
	}
}

func (s *StreamOutAdapter) Write(p []byte) (n int, err error) {
	return 0, verrors.New("stream-out-adapter-writer-not-implemented")
}

func (s *StreamOutAdapter) Close() error {
	s.logger.Info("stream-out-adapter-close")
	err := s.client.CloseSend()
	if err != nil {
		s.logger.Error("stream-out-adapter-client-close-failed", err)
		return verrors.New("stream-out-adapter-close-failed")
	}
	return nil
}
