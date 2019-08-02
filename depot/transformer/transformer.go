package transformer

import (
	"bytes"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"code.cloudfoundry.org/archiver/compressor"
	"code.cloudfoundry.org/bbs/models"
	"code.cloudfoundry.org/cacheddownloader"
	"code.cloudfoundry.org/clock"
	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/log_streamer"
	"code.cloudfoundry.org/executor/depot/steps"
	"code.cloudfoundry.org/executor/depot/uploader"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/workpool"
	"github.com/tedsuo/ifrit"
)

const (
	healthCheckNofiles                          uint64 = 1024
	DefaultDeclarativeHealthcheckRequestTimeout        = int(1 * time.Second / time.Millisecond)
	HealthLogSource                                    = "HEALTH"
)

var ErrNoCheck = errors.New("no check configured")
var HealthCheckDstPath string = filepath.Join(string(os.PathSeparator), "etc", "cf-assets", "healthcheck")

//go:generate counterfeiter -o faketransformer/fake_transformer.go . Transformer

type Transformer interface {
	StepsRunner(lager.Logger, executor.Container, garden.Container, log_streamer.LogStreamer, Config) (ifrit.Runner, error)
}

type Config struct {
	ProxyTLSPorts     []uint16
	BindMounts        []garden.BindMount
	CreationStartTime time.Time
	MetronClient      loggingclient.IngressClient
}

type transformer struct {
	cachedDownloader cacheddownloader.CachedDownloader
	uploader         uploader.Uploader
	compressor       compressor.Compressor
	downloadLimiter  chan struct{}
	uploadLimiter    chan struct{}
	tempDir          string
	clock            clock.Clock

	sidecarRootFS               string
	useDeclarativeHealthCheck   bool
	healthyMonitoringInterval   time.Duration
	unhealthyMonitoringInterval time.Duration
	gracefulShutdownInterval    time.Duration
	healthCheckWorkPool         *workpool.WorkPool

	useContainerProxy bool
	drainWait         time.Duration

	postSetupHook []string
	postSetupUser string
}

type Option func(*transformer)

func WithSidecarRootfs(
	sidecarRootFS string,
) Option {
	return func(t *transformer) {
		t.sidecarRootFS = sidecarRootFS
	}
}

func WithDeclarativeHealthchecks() Option {
	return func(t *transformer) {
		t.useDeclarativeHealthCheck = true
	}
}

func WithContainerProxy(drainWait time.Duration) Option {
	return func(t *transformer) {
		t.useContainerProxy = true
		t.drainWait = drainWait
	}
}

func WithPostSetupHook(user string, hook []string) Option {
	return func(t *transformer) {
		t.postSetupUser = user
		t.postSetupHook = hook
	}
}

func NewTransformer(
	clock clock.Clock,
	cachedDownloader cacheddownloader.CachedDownloader,
	uploader uploader.Uploader,
	compressor compressor.Compressor,
	downloadLimiter chan struct{},
	uploadLimiter chan struct{},
	tempDir string,
	healthyMonitoringInterval time.Duration,
	unhealthyMonitoringInterval time.Duration,
	gracefulShutdownInterval time.Duration,
	healthCheckWorkPool *workpool.WorkPool,
	opts ...Option,
) *transformer {
	t := &transformer{
		cachedDownloader:            cachedDownloader,
		uploader:                    uploader,
		compressor:                  compressor,
		downloadLimiter:             downloadLimiter,
		uploadLimiter:               uploadLimiter,
		tempDir:                     tempDir,
		healthyMonitoringInterval:   healthyMonitoringInterval,
		unhealthyMonitoringInterval: unhealthyMonitoringInterval,
		gracefulShutdownInterval:    gracefulShutdownInterval,
		healthCheckWorkPool:         healthCheckWorkPool,
		clock:                       clock,
	}

	for _, o := range opts {
		o(t)
	}

	return t
}

func (t *transformer) stepFor(
	logStreamer log_streamer.LogStreamer,
	action *models.Action,
	container garden.Container,
	externalIP string,
	internalIP string,
	ports []executor.PortMapping,
	suppressExitStatusCode bool,
	monitorOutputWrapper bool,
	logger lager.Logger,
) ifrit.Runner {
	a := action.GetValue()
	switch actionModel := a.(type) {
	case *models.RunAction:
		return steps.NewRun(
			container,
			*actionModel,
			logStreamer.WithSource(actionModel.LogSource),
			logger,
			externalIP,
			internalIP,
			ports,
			t.clock,
			t.gracefulShutdownInterval,
			suppressExitStatusCode,
		)

	case *models.DownloadAction:
		return steps.NewDownload(
			container,
			*actionModel,
			t.cachedDownloader,
			t.downloadLimiter,
			logStreamer.WithSource(actionModel.LogSource),
			logger,
		)

	case *models.UploadAction:
		return steps.NewUpload(
			container,
			*actionModel,
			t.uploader,
			t.compressor,
			t.tempDir,
			logStreamer.WithSource(actionModel.LogSource),
			t.uploadLimiter,
			logger,
		)

	case *models.EmitProgressAction:
		return steps.NewEmitProgress(
			t.stepFor(
				logStreamer,
				actionModel.Action,
				container,
				externalIP,
				internalIP,
				ports,
				suppressExitStatusCode,
				monitorOutputWrapper,
				logger,
			),
			actionModel.StartMessage,
			actionModel.SuccessMessage,
			actionModel.FailureMessagePrefix,
			logStreamer.WithSource(actionModel.LogSource),
			logger,
		)

	case *models.TimeoutAction:
		return steps.NewTimeout(
			t.stepFor(
				logStreamer.WithSource(actionModel.LogSource),
				actionModel.Action,
				container,
				externalIP,
				internalIP,
				ports,
				suppressExitStatusCode,
				monitorOutputWrapper,
				logger,
			),
			time.Duration(actionModel.TimeoutMs)*time.Millisecond,
			t.clock,
			logger,
		)

	case *models.TryAction:
		return steps.NewTry(
			t.stepFor(
				logStreamer.WithSource(actionModel.LogSource),
				actionModel.Action,
				container,
				externalIP,
				internalIP,
				ports,
				suppressExitStatusCode,
				monitorOutputWrapper,
				logger,
			),
			logger,
		)

	case *models.ParallelAction:
		subSteps := make([]ifrit.Runner, len(actionModel.Actions))
		for i, action := range actionModel.Actions {
			var subStep ifrit.Runner
			if monitorOutputWrapper {
				buffer := log_streamer.NewConcurrentBuffer(bytes.NewBuffer(nil))
				bufferedLogStreamer := log_streamer.NewBufferStreamer(buffer, ioutil.Discard)
				subStep = steps.NewOutputWrapper(t.stepFor(
					bufferedLogStreamer,
					action,
					container,
					externalIP,
					internalIP,
					ports,
					suppressExitStatusCode,
					monitorOutputWrapper,
					logger,
				),
					buffer,
				)
			} else {
				subStep = t.stepFor(
					logStreamer.WithSource(actionModel.LogSource),
					action,
					container,
					externalIP,
					internalIP,
					ports,
					suppressExitStatusCode,
					monitorOutputWrapper,
					logger,
				)
			}
			subSteps[i] = subStep
		}
		return steps.NewParallel(subSteps)

	case *models.CodependentAction:
		subSteps := make([]ifrit.Runner, len(actionModel.Actions))
		for i, action := range actionModel.Actions {
			var subStep ifrit.Runner
			if monitorOutputWrapper {
				buffer := log_streamer.NewConcurrentBuffer(bytes.NewBuffer(nil))
				bufferedLogStreamer := log_streamer.NewBufferStreamer(buffer, ioutil.Discard)
				subStep = steps.NewOutputWrapper(t.stepFor(
					bufferedLogStreamer,
					action,
					container,
					externalIP,
					internalIP,
					ports,
					suppressExitStatusCode,
					monitorOutputWrapper,
					logger,
				),
					buffer,
				)
			} else {
				subStep = t.stepFor(
					logStreamer.WithSource(actionModel.LogSource),
					action,
					container,
					externalIP,
					internalIP,
					ports,
					suppressExitStatusCode,
					monitorOutputWrapper,
					logger,
				)
			}
			subSteps[i] = subStep
		}
		errorOnExit := true
		return steps.NewCodependent(subSteps, errorOnExit, false)

	case *models.SerialAction:
		subSteps := make([]ifrit.Runner, len(actionModel.Actions))
		for i, action := range actionModel.Actions {
			subSteps[i] = t.stepFor(
				logStreamer,
				action,
				container,
				externalIP,
				internalIP,
				ports,
				suppressExitStatusCode,
				monitorOutputWrapper,
				logger,
			)
		}
		return steps.NewSerial(subSteps)
	}

	panic(fmt.Sprintf("unknown action: %T", action))
}

func overrideSuppressLogOutput(monitorAction *models.Action) {
	if monitorAction.RunAction != nil {
		monitorAction.RunAction.SuppressLogOutput = false
	} else if monitorAction.TryAction != nil {
		overrideSuppressLogOutput(monitorAction.TryAction.Action)
	} else if monitorAction.ParallelAction != nil {
		for _, action := range monitorAction.ParallelAction.Actions {
			overrideSuppressLogOutput(action)
		}
	} else if monitorAction.SerialAction != nil {
		for _, action := range monitorAction.SerialAction.Actions {
			overrideSuppressLogOutput(action)
		}
	} else if monitorAction.CodependentAction != nil {
		for _, action := range monitorAction.CodependentAction.Actions {
			overrideSuppressLogOutput(action)
		}
	} else if monitorAction.EmitProgressAction != nil {
		overrideSuppressLogOutput(monitorAction.EmitProgressAction.Action)
	} else if monitorAction.TimeoutAction != nil {
		overrideSuppressLogOutput(monitorAction.TimeoutAction.Action)
	}
}
func (t *transformer) StepsRunner(
	logger lager.Logger,
	container executor.Container,
	gardenContainer garden.Container,
	logStreamer log_streamer.LogStreamer,
	config Config,
) (ifrit.Runner, error) {
	var setup, action, postSetup, monitor, longLivedAction ifrit.Runner
	var substeps []ifrit.Runner

	if container.Setup != nil {
		setup = t.stepFor(
			logStreamer,
			container.Setup,
			gardenContainer,
			container.ExternalIP,
			container.InternalIP,
			container.Ports,
			false,
			false,
			logger.Session("setup"),
		)
	}
	setup = steps.NewTimedStep(logger, setup, config.MetronClient, t.clock, config.CreationStartTime)

	if len(t.postSetupHook) > 0 {
		actionModel := models.RunAction{
			Path: t.postSetupHook[0],
			Args: t.postSetupHook[1:],
			User: t.postSetupUser,
		}
		suppressExitStatusCode := false
		postSetup = steps.NewRun(
			gardenContainer,
			actionModel,
			log_streamer.NewNoopStreamer(),
			logger.Session("post-setup"),
			container.ExternalIP,
			container.InternalIP,
			container.Ports,
			t.clock,
			t.gracefulShutdownInterval,
			suppressExitStatusCode,
		)
	}

	if container.Action == nil {
		err := errors.New("container cannot have empty action")
		logger.Error("steps-runner-empty-action", err)
		return nil, err
	}

	action = t.stepFor(
		logStreamer,
		container.Action,
		gardenContainer,
		container.ExternalIP,
		container.InternalIP,
		container.Ports,
		false,
		false,
		logger.Session("action"),
	)

	substeps = append(substeps, action)

	for _, sidecar := range container.Sidecars {
		substeps = append(substeps, t.stepFor(logStreamer,
			sidecar.Action,
			gardenContainer,
			container.ExternalIP,
			container.InternalIP,
			container.Ports,
			false,
			false,
			logger.Session("sidecar"),
		))
	}

	var proxyReadinessChecks []ifrit.Runner

	if t.useContainerProxy && t.useDeclarativeHealthCheck {
		envoyReadinessLogger := logger.Session("envoy-readiness-check")

		for idx, p := range config.ProxyTLSPorts {
			// add envoy readiness checks
			readinessSidecarName := fmt.Sprintf("%s-envoy-readiness-healthcheck-%d", gardenContainer.Handle(), idx)

			step := t.createCheck(
				&container,
				gardenContainer,
				config.BindMounts,
				"",
				readinessSidecarName,
				int(p),
				DefaultDeclarativeHealthcheckRequestTimeout,
				false,
				true,
				t.unhealthyMonitoringInterval,
				envoyReadinessLogger,
				"instance proxy failed to start",
			)
			proxyReadinessChecks = append(proxyReadinessChecks, step)
		}
	}

	if container.CheckDefinition != nil && t.useDeclarativeHealthCheck {
		monitor = t.transformCheckDefinition(logger,
			&container,
			gardenContainer,
			logStreamer,
			config.BindMounts,
			proxyReadinessChecks,
		)
		substeps = append(substeps, monitor)
	} else if container.Monitor != nil {
		overrideSuppressLogOutput(container.Monitor)
		monitor = steps.NewMonitor(
			func() ifrit.Runner {
				return t.stepFor(
					logStreamer,
					container.Monitor,
					gardenContainer,
					container.ExternalIP,
					container.InternalIP,
					container.Ports,
					true,
					true,
					logger.Session("monitor-run"),
				)
			},
			logger.Session("monitor"),
			t.clock,
			logStreamer,
			time.Duration(container.StartTimeoutMs)*time.Millisecond,
			t.healthyMonitoringInterval,
			t.unhealthyMonitoringInterval,
			t.healthCheckWorkPool,
			proxyReadinessChecks...,
		)
		substeps = append(substeps, monitor)
	}

	if len(substeps) > 1 {
		longLivedAction = steps.NewCodependent(substeps, false, false)
	} else {
		longLivedAction = action
	}

	if t.useContainerProxy && container.EnableContainerProxy {
		containerProxyStep := t.transformContainerProxyStep(
			gardenContainer,
			container,
			logger,
			logStreamer,
			config.BindMounts,
		)
		longLivedAction = steps.NewCodependent([]ifrit.Runner{longLivedAction, containerProxyStep}, false, true)
	}

	var cumulativeStep ifrit.Runner
	if setup == nil {
		cumulativeStep = longLivedAction
	} else {
		if postSetup == nil {
			cumulativeStep = steps.NewSerial([]ifrit.Runner{setup, longLivedAction})
		} else {
			cumulativeStep = steps.NewSerial([]ifrit.Runner{setup, postSetup, longLivedAction})
		}
	}

	return cumulativeStep, nil
}

func (t *transformer) createCheck(
	container *executor.Container,
	gardenContainer garden.Container,
	bindMounts []garden.BindMount,
	path,
	sidecarName string,
	port,
	timeout int,
	http,
	readiness bool,
	interval time.Duration,
	logger lager.Logger,
	prefix string,
) ifrit.Runner {

	nofiles := healthCheckNofiles

	sourceName := HealthLogSource
	if container.CheckDefinition != nil && container.CheckDefinition.LogSource != "" {
		sourceName = container.CheckDefinition.LogSource
	}

	args := []string{
		fmt.Sprintf("-port=%d", port),
		fmt.Sprintf("-timeout=%dms", timeout),
	}

	if http {
		args = append(args, fmt.Sprintf("-uri=%s", path))
	}

	if readiness {
		args = append(args, fmt.Sprintf("-readiness-interval=%s", interval))
		args = append(args, fmt.Sprintf("-readiness-timeout=%s", time.Duration(container.StartTimeoutMs)*time.Millisecond))
	} else {
		args = append(args, fmt.Sprintf("-liveness-interval=%s", interval))
	}

	rl := models.ResourceLimits{}
	rl.SetNofile(nofiles)
	runAction := models.RunAction{
		LogSource:      sourceName,
		ResourceLimits: &rl,
		Path:           filepath.Join(HealthCheckDstPath, "healthcheck"),
		Args:           args,
	}

	buffer := bytes.NewBuffer(nil)
	bufferedLogStreamer := log_streamer.NewBufferStreamer(buffer, ioutil.Discard)
	sidecar := steps.Sidecar{
		Name:                    sidecarName,
		Image:                   garden.ImageRef{URI: t.sidecarRootFS},
		BindMounts:              bindMounts,
		OverrideContainerLimits: &garden.ProcessLimits{},
	}
	runStep := steps.NewRunWithSidecar(gardenContainer,
		runAction,
		bufferedLogStreamer,
		logger,
		container.ExternalIP,
		container.InternalIP,
		container.Ports,
		t.clock,
		t.gracefulShutdownInterval,
		true,
		sidecar,
		container.Privileged,
	)
	if prefix != "" {
		return steps.NewOutputWrapperWithPrefix(runStep, buffer, prefix)
	}
	return steps.NewOutputWrapper(runStep, buffer)
}

func (t *transformer) transformCheckDefinition(
	logger lager.Logger,
	container *executor.Container,
	gardenContainer garden.Container,
	logstreamer log_streamer.LogStreamer,
	bindMounts []garden.BindMount,
	proxyReadinessChecks []ifrit.Runner,
) ifrit.Runner {
	var readinessChecks []ifrit.Runner
	var livenessChecks []ifrit.Runner

	sourceName := HealthLogSource
	if container.CheckDefinition.LogSource != "" {
		sourceName = container.CheckDefinition.LogSource
	}

	logger.Info("transform-check-definitions-starting")
	defer func() {
		logger.Info("transform-check-definitions-finished")
	}()

	readinessLogger := logger.Session("readiness-check")
	livenessLogger := logger.Session("liveness-check")

	for index, check := range container.CheckDefinition.Checks {

		readinessSidecarName := fmt.Sprintf("%s-readiness-healthcheck-%d", gardenContainer.Handle(), index)
		livenessSidecarName := fmt.Sprintf("%s-liveness-healthcheck-%d", gardenContainer.Handle(), index)

		if err := check.Validate(); err != nil {
			logger.Error("invalid-check", err, lager.Data{"check": check})
		} else if check.HttpCheck != nil {
			timeout := int(check.HttpCheck.RequestTimeoutMs)
			if timeout == 0 {
				timeout = DefaultDeclarativeHealthcheckRequestTimeout
			}
			path := check.HttpCheck.Path
			if path == "" {
				path = "/"
			}

			readinessChecks = append(readinessChecks, t.createCheck(
				container,
				gardenContainer,
				bindMounts,
				path,
				readinessSidecarName,
				int(check.HttpCheck.Port),
				timeout,
				true,
				true,
				t.unhealthyMonitoringInterval,
				readinessLogger,
				"",
			))
			livenessChecks = append(livenessChecks, t.createCheck(
				container,
				gardenContainer,
				bindMounts,
				path,
				livenessSidecarName,
				int(check.HttpCheck.Port),
				timeout,
				true,
				false,
				t.healthyMonitoringInterval,
				livenessLogger,
				"",
			))

		} else if check.TcpCheck != nil {
			timeout := int(check.TcpCheck.ConnectTimeoutMs)
			if timeout == 0 {
				timeout = DefaultDeclarativeHealthcheckRequestTimeout
			}

			readinessChecks = append(readinessChecks, t.createCheck(
				container,
				gardenContainer,
				bindMounts,
				"",
				readinessSidecarName,
				int(check.TcpCheck.Port),
				timeout,
				false,
				true,
				t.unhealthyMonitoringInterval,
				readinessLogger,
				"",
			))
			livenessChecks = append(livenessChecks, t.createCheck(
				container,
				gardenContainer,
				bindMounts,
				"",
				livenessSidecarName,
				int(check.TcpCheck.Port),
				timeout,
				false,
				false,
				t.healthyMonitoringInterval,
				livenessLogger,
				"",
			))
		}
	}

	readinessCheck := steps.NewParallel(append(proxyReadinessChecks, readinessChecks...))
	livenessCheck := steps.NewCodependent(livenessChecks, false, false)

	return steps.NewHealthCheckStep(
		readinessCheck,
		livenessCheck,
		logger,
		t.clock,
		logstreamer,
		logstreamer.WithSource(sourceName),
		time.Duration(container.StartTimeoutMs)*time.Millisecond,
	)
}

func (t *transformer) transformContainerProxyStep(
	container garden.Container,
	execContainer executor.Container,
	logger lager.Logger,
	streamer log_streamer.LogStreamer,
	bindMounts []garden.BindMount,
) ifrit.Runner {

	envoyArgs := []string{
		"-c", "/etc/cf-assets/envoy_config/envoy.yaml",
		"--drain-time-s", strconv.Itoa(int(t.drainWait.Seconds())),
		"--log-level", "critical",
	}

	runAction := envoyRunAction(envoyArgs)

	sidecar := steps.Sidecar{
		Image:      garden.ImageRef{URI: t.sidecarRootFS},
		BindMounts: bindMounts,
		Name:       fmt.Sprintf("%s-envoy", container.Handle()),
	}

	proxyLogger := logger.Session("proxy")

	return steps.NewBackground(steps.NewRunWithSidecar(
		container,
		runAction,
		streamer.WithSource("PROXY"),
		proxyLogger,
		execContainer.ExternalIP,
		execContainer.InternalIP,
		execContainer.Ports,
		t.clock,
		t.gracefulShutdownInterval,
		false,
		sidecar,
		execContainer.Privileged,
	), proxyLogger)
}
