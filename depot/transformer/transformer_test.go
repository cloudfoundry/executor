package transformer_test

import (
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/bbs/models"
	"code.cloudfoundry.org/clock/fakeclock"
	mfakes "code.cloudfoundry.org/diego-logging-client/testhelpers"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/log_streamer"
	"code.cloudfoundry.org/executor/depot/transformer"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/garden/gardenfakes"
	"code.cloudfoundry.org/lager/v3"
	"code.cloudfoundry.org/lager/v3/lagertest"
	"code.cloudfoundry.org/workpool"
	"github.com/gogo/protobuf/proto"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"
	"github.com/tedsuo/ifrit"
	ginkgomon "github.com/tedsuo/ifrit/ginkgomon_v2"
)

var _ = Describe("Transformer", func() {
	Describe("StepsRunner", func() {
		var (
			logger                      lager.Logger
			optimusPrime                transformer.Transformer
			container                   executor.Container
			logStreamer                 log_streamer.LogStreamer
			gardenContainer             *gardenfakes.FakeContainer
			clock                       *fakeclock.FakeClock
			fakeMetronClient            *mfakes.FakeIngressClient
			healthyMonitoringInterval   time.Duration
			unhealthyMonitoringInterval time.Duration
			gracefulShutdownInterval    time.Duration
			healthCheckWorkPool         *workpool.WorkPool
			cfg                         transformer.Config
			options                     []transformer.Option
		)

		BeforeEach(func() {
			gardenContainer = &gardenfakes.FakeContainer{}
			fakeMetronClient = &mfakes.FakeIngressClient{}

			logger = lagertest.NewTestLogger("test-container-store")

			logConfig := executor.LogConfig{Guid: "test", SourceName: "test", Index: 1, Tags: map[string]string{}}
			logStreamer = log_streamer.New(logConfig, fakeMetronClient, 100, 100000, 5*time.Minute)

			healthyMonitoringInterval = 1 * time.Second
			unhealthyMonitoringInterval = 1 * time.Millisecond
			gracefulShutdownInterval = 10 * time.Second

			var err error
			healthCheckWorkPool, err = workpool.NewWorkPool(10)
			Expect(err).NotTo(HaveOccurred())

			clock = fakeclock.NewFakeClock(time.Now())

			cfg = transformer.Config{
				MetronClient: fakeMetronClient,
				BindMounts: []garden.BindMount{
					{
						SrcPath: "/some/source",
						DstPath: "/some/destintation",
						Mode:    garden.BindMountModeRO,
						Origin:  garden.BindMountOriginHost,
					},
				},
			}

			options = []transformer.Option{
				transformer.WithSidecarRootfs("preloaded:cflinuxfs3"),
			}

			container = executor.Container{
				RunInfo: executor.RunInfo{
					Setup: &models.Action{
						RunAction: &models.RunAction{
							Path: "/setup/path",
						},
					},
					Action: &models.Action{
						RunAction: &models.RunAction{
							Path: "/action/path",
						},
					},
					Monitor: &models.Action{
						RunAction: &models.RunAction{
							Path: "/monitor/path",
						},
					},
				},
			}
		})

		JustBeforeEach(func() {
			optimusPrime = transformer.NewTransformer(
				clock,
				nil, nil, nil, nil, nil,
				os.TempDir(),
				healthyMonitoringInterval,
				unhealthyMonitoringInterval,
				gracefulShutdownInterval,
				healthCheckWorkPool,
				options...,
			)
		})

		Context("when there is no run action", func() {
			BeforeEach(func() {
				container.Action = nil
			})

			It("returns an error", func() {
				_, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when there is a specified setup, post-setup, action, sidecars and monitor", func() {
			BeforeEach(func() {
				options = []transformer.Option{
					transformer.WithPostSetupHook(
						"jim",
						[]string{"/post-setup/path", "-x", "argument"},
					),
				}
				container.Sidecars = []executor.Sidecar{
					{
						Action: &models.Action{
							RunAction: &models.RunAction{
								Path: "/sidecar-action-1",
							},
						},
					},
					{
						Action: &models.Action{
							RunAction: &models.RunAction{
								Path: "/sidecar-action-2",
							},
						},
					},
				}
			})

			It("returns a step encapsulating setup, post-setup, action, sidecars and monitor", func() {
				setupReceived := make(chan struct{})
				postSetupReceived := make(chan struct{})
				gardenContainer.RunStub = func(processSpec garden.ProcessSpec, processIO garden.ProcessIO) (garden.Process, error) {
					if processSpec.Path == "/setup/path" {
						setupReceived <- struct{}{}
					} else if processSpec.Path == "/post-setup/path" {
						postSetupReceived <- struct{}{}
					}
					return &gardenfakes.FakeProcess{}, nil
				}

				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
				Expect(err).NotTo(HaveOccurred())

				process := ifrit.Background(runner)

				Eventually(gardenContainer.RunCallCount).Should(Equal(1))
				processSpec, _ := gardenContainer.RunArgsForCall(0)
				Expect(processSpec.Path).To(Equal("/setup/path"))
				Consistently(gardenContainer.RunCallCount).Should(Equal(1))

				<-setupReceived

				Eventually(gardenContainer.RunCallCount).Should(Equal(2))
				processSpec, _ = gardenContainer.RunArgsForCall(1)
				Expect(processSpec.Path).To(Equal("/post-setup/path"))
				Expect(processSpec.Args).To(Equal([]string{"-x", "argument"}))
				Expect(processSpec.User).To(Equal("jim"))
				Consistently(gardenContainer.RunCallCount).Should(Equal(2))

				<-postSetupReceived

				Eventually(gardenContainer.RunCallCount).Should(Equal(5))

				processPaths := []string{}
				for i := 2; i < 5; i++ {
					processSpec, _ = gardenContainer.RunArgsForCall(i)
					processPaths = append(processPaths, processSpec.Path)
				}
				Expect(processPaths).To(ConsistOf("/action/path", "/sidecar-action-1", "/sidecar-action-2"))

				Consistently(gardenContainer.RunCallCount).Should(Equal(5))

				clock.Increment(1 * time.Second)
				Eventually(gardenContainer.RunCallCount).Should(Equal(6))
				processSpec, processIO := gardenContainer.RunArgsForCall(5)
				Expect(processSpec.Path).To(Equal("/monitor/path"))
				Expect(container.Monitor.RunAction.GetSuppressLogOutput()).Should(BeFalse())
				Expect(processIO.Stdout).ShouldNot(Equal(ioutil.Discard))
				Expect(processIO.Stderr).ShouldNot(Equal(ioutil.Discard))

				process.Signal(os.Interrupt)
				clock.Increment(1 * time.Second)
				Eventually(process.Wait()).Should(Receive(nil))
			})
		})

		It("logs container setup time", func() {
			gardenContainer.RunStub = func(processSpec garden.ProcessSpec, processIO garden.ProcessIO) (garden.Process, error) {
				if processSpec.Path == "/setup/path" {
					clock.Increment(1 * time.Second)
				}
				return &gardenfakes.FakeProcess{}, nil
			}

			cfg.CreationStartTime = clock.Now()
			runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
			Expect(err).NotTo(HaveOccurred())
			ifrit.Background(runner)

			Eventually(logger).Should(gbytes.Say("container-setup.*duration.*1000000000"))
		})

		It("does not become ready until the healthcheck passes", func() {
			monitorProcess := &gardenfakes.FakeProcess{}
			monitorProcess.WaitStub = func() (int, error) {
				if monitorProcess.WaitCallCount() == 1 {
					return 1, errors.New("boom")
				}
				return 0, nil
			}
			gardenContainer.RunStub = func(processSpec garden.ProcessSpec, processIO garden.ProcessIO) (garden.Process, error) {
				if processSpec.Path == "/monitor/path" {
					return monitorProcess, nil
				}
				return &gardenfakes.FakeProcess{}, nil
			}
			runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
			Expect(err).NotTo(HaveOccurred())
			process := ifrit.Background(runner)
			Consistently(process.Ready()).ShouldNot(BeClosed())

			clock.Increment(1 * time.Second)
			Consistently(process.Ready()).ShouldNot(BeClosed())

			clock.Increment(1 * time.Second)
			Eventually(process.Ready()).Should(BeClosed())
		})

		makeProcess := func(waitCh chan int) *gardenfakes.FakeProcess {
			process := &gardenfakes.FakeProcess{}
			process.WaitStub = func() (int, error) {
				return <-waitCh, nil
			}
			return process
		}

		Describe("container proxy", func() {
			var (
				container    executor.Container
				processLock  sync.Mutex
				process      ifrit.Process
				envoyProcess *gardenfakes.FakeProcess
				actionCh     chan int
				envoyCh      chan int
			)

			BeforeEach(func() {
				options = append(options, transformer.WithContainerProxy(time.Second))

				processLock.Lock()
				defer processLock.Unlock()

				actionCh = make(chan int, 1)
				actionProcess := makeProcess(actionCh)

				envoyCh = make(chan int)
				envoyProcess = makeProcess(envoyCh)

				gardenContainer.RunStub = func(spec garden.ProcessSpec, io garden.ProcessIO) (process garden.Process, err error) {
					defer GinkgoRecover()
					// get rid of race condition caused by write inside the BeforeEach

					processLock.Lock()
					defer processLock.Unlock()

					switch {
					case spec.Path == "/action/path":
						return actionProcess, nil
					case spec.Path == "sh" && runtime.GOOS != "windows":
						return envoyProcess, nil
					case spec.Path == "/etc/cf-assets/envoy/envoy" && runtime.GOOS == "windows":
						return envoyProcess, nil
					}

					err = errors.New("")
					Fail("unexpected executable path: " + spec.Path)
					return nil, err
				}

				container = executor.Container{
					ExternalIP: "10.0.0.1",
					InternalIP: "11.0.0.1",
					RunInfo: executor.RunInfo{
						Action: &models.Action{
							RunAction: &models.RunAction{
								Path: "/action/path",
							},
						},
						Monitor: &models.Action{
							RunAction: &models.RunAction{
								Path: "/monitor/path",
							},
						},
						CheckDefinition: &models.CheckDefinition{
							Checks: []*models.Check{
								&models.Check{
									HttpCheck: &models.HTTPCheck{
										Port:             5432,
										RequestTimeoutMs: 100,
										Path:             "/some/path",
									},
								},
							},
						},
						Ports: []executor.PortMapping{
							executor.PortMapping{
								HostPort:      61001,
								ContainerPort: 8080,
							},
							executor.PortMapping{
								HostPort:      61002,
								ContainerPort: 61001,
							},
						},
						EnableContainerProxy: true,
					},
				}
			})

			JustBeforeEach(func() {
				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
				Expect(err).NotTo(HaveOccurred())

				process = ifrit.Background(runner)
			})

			AfterEach(func() {
				close(actionCh)
				close(envoyCh)
				ginkgomon.Interrupt(process)
			})

			It("runs the container proxy in a sidecar container", func() {
				Eventually(gardenContainer.RunCallCount).Should(Equal(2))
				specs := []garden.ProcessSpec{}
				for i := 0; i < gardenContainer.RunCallCount(); i++ {
					spec, _ := gardenContainer.RunArgsForCall(i)
					specs = append(specs, spec)
				}

				envoyArgs := "-c /etc/cf-assets/envoy_config/envoy.yaml --drain-time-s 1 --log-level critical"

				path := "sh"
				args := []string{
					"-c",
					fmt.Sprintf("trap 'kill -9 0' TERM; /etc/cf-assets/envoy/envoy %s& pid=$!; wait $pid", envoyArgs),
				}

				if runtime.GOOS == "windows" {
					path = "/etc/cf-assets/envoy/envoy"
					envoyArgs += " --id-creds C:\\etc\\cf-assets\\envoy_config\\sds-id-cert-and-key.yaml"
					envoyArgs += " --c2c-creds C:\\etc\\cf-assets\\envoy_config\\sds-c2c-cert-and-key.yaml"
					envoyArgs += " --id-validation C:\\etc\\cf-assets\\envoy_config\\sds-id-validation-context.yaml"
					args = strings.Split(envoyArgs, " ")
				}

				Expect(specs).To(ContainElement(garden.ProcessSpec{
					ID:   fmt.Sprintf("%s-envoy", gardenContainer.Handle()),
					Path: path,
					Args: args,

					Env: []string{
						"CF_INSTANCE_IP=10.0.0.1",
						"CF_INSTANCE_INTERNAL_IP=11.0.0.1",
						"CF_INSTANCE_PORT=61001",
						"CF_INSTANCE_ADDR=10.0.0.1:61001",
						"CF_INSTANCE_PORTS=[{\"external\":61001,\"internal\":8080},{\"external\":61002,\"internal\":61001}]",
					},
					Image: garden.ImageRef{URI: "preloaded:cflinuxfs3"},
					BindMounts: []garden.BindMount{
						{
							SrcPath: "/some/source",
							DstPath: "/some/destintation",
							Mode:    garden.BindMountModeRO,
							Origin:  garden.BindMountOriginHost,
						},
					},
				}))
			})

			Context("when the process is signalled", func() {
				JustBeforeEach(func() {
					Eventually(gardenContainer.RunCallCount).Should(Equal(2))
					process.Signal(os.Interrupt)
				})

				It("does not signal the proxy process", func() {
					Consistently(envoyProcess.SignalCallCount).Should(BeZero())
				})
			})

			Context("when the envoy process is signalled", func() {
				JustBeforeEach(func() {
					Eventually(gardenContainer.RunCallCount).Should(Equal(2))
					envoyCh <- 134
				})

				It("logs the exit status", func() {
					Eventually(fakeMetronClient.SendAppLogCallCount).Should(Equal(2))
					msg0, _, _ := fakeMetronClient.SendAppLogArgsForCall(0)
					msg1, _, _ := fakeMetronClient.SendAppLogArgsForCall(1)
					Expect([]string{msg0, msg1}).To(ContainElement("Exit status 134"))
				})

				It("process should fail with a descriptive error", func() {
					actionCh <- 0
					Eventually(process.Wait()).Should(Receive(MatchError("PROXY: Exited with status 134")))
				})
			})

			Context("when the container is privileged", func() {
				BeforeEach(func() {
					container.Privileged = true
				})

				It("runs the container proxy in a sidecar container", func() {
					Eventually(gardenContainer.RunCallCount).Should(Equal(2))
					specs := []garden.ProcessSpec{}
					for i := 0; i < gardenContainer.RunCallCount(); i++ {
						spec, _ := gardenContainer.RunArgsForCall(i)
						specs = append(specs, spec)
					}

					envoyArgs := "-c /etc/cf-assets/envoy_config/envoy.yaml --drain-time-s 1 --log-level critical"

					path := "sh"
					args := []string{
						"-c",
						fmt.Sprintf("trap 'kill -9 0' TERM; /etc/cf-assets/envoy/envoy %s& pid=$!; wait $pid", envoyArgs),
					}

					if runtime.GOOS == "windows" {
						path = "/etc/cf-assets/envoy/envoy"
						envoyArgs += " --id-creds C:\\etc\\cf-assets\\envoy_config\\sds-id-cert-and-key.yaml"
						envoyArgs += " --c2c-creds C:\\etc\\cf-assets\\envoy_config\\sds-c2c-cert-and-key.yaml"
						envoyArgs += " --id-validation C:\\etc\\cf-assets\\envoy_config\\sds-id-validation-context.yaml"
						args = strings.Split(envoyArgs, " ")
					}

					Expect(specs).To(ContainElement(garden.ProcessSpec{
						ID:   fmt.Sprintf("%s-envoy", gardenContainer.Handle()),
						Path: path,
						Args: args,

						Env: []string{
							"CF_INSTANCE_IP=10.0.0.1",
							"CF_INSTANCE_INTERNAL_IP=11.0.0.1",
							"CF_INSTANCE_PORT=61001",
							"CF_INSTANCE_ADDR=10.0.0.1:61001",
							"CF_INSTANCE_PORTS=[{\"external\":61001,\"internal\":8080},{\"external\":61002,\"internal\":61001}]",
						},
						Image: garden.ImageRef{URI: "preloaded:cflinuxfs3"},
						BindMounts: []garden.BindMount{
							{
								SrcPath: "/some/source",
								DstPath: "/some/destintation",
								Mode:    garden.BindMountModeRO,
								Origin:  garden.BindMountOriginHost,
							},
						},
					}))
				})
			})

			Context("when the container proxy is disabled on the container", func() {
				BeforeEach(func() {
					container.EnableContainerProxy = false
				})

				It("does not run the container proxy", func() {
					Eventually(gardenContainer.RunCallCount).Should(Equal(1))
					paths := []string{}
					for i := 0; i < gardenContainer.RunCallCount(); i++ {
						spec, _ := gardenContainer.RunArgsForCall(i)
						paths = append(paths, spec.Path)
					}

					Expect(paths).NotTo(ContainElement("sh"))
				})
			})
		})

		Describe("declarative healthchecks", func() {
			var (
				process                       ifrit.Process
				startupProcess                *gardenfakes.FakeProcess
				startupCh                     chan int
				livenessProcess               *gardenfakes.FakeProcess
				livenessCh                    chan int
				actionProcess                 *gardenfakes.FakeProcess
				actionCh                      chan int
				monitorProcess                *gardenfakes.FakeProcess
				monitorCh                     chan int
				startupIO                     chan garden.ProcessIO
				livenessIO                    chan garden.ProcessIO
				processLock                   sync.Mutex
				specs                         chan garden.ProcessSpec
				declarativeHealthcheckSrcPath string = filepath.Join(string(os.PathSeparator), "dir", "healthcheck")
			)

			BeforeEach(func() {
				// get rid of race condition caused by read inside the RunStub
				processLock.Lock()
				defer processLock.Unlock()

				startupIO = make(chan garden.ProcessIO, 1)
				livenessIO = make(chan garden.ProcessIO, 1)
				specs = make(chan garden.ProcessSpec, 10)
				// make the race detector happy
				startupIOCh := startupIO
				livenessIOCh := livenessIO
				specsCh := specs

				startupCh = make(chan int)
				startupProcess = makeProcess(startupCh)

				livenessCh = make(chan int, 1)
				livenessProcess = makeProcess(livenessCh)

				actionCh = make(chan int, 1)
				actionProcess = makeProcess(actionCh)

				monitorCh = make(chan int)
				monitorProcess = makeProcess(monitorCh)

				healthcheckCallCount := int64(0)
				gardenContainer.RunStub = func(spec garden.ProcessSpec, io garden.ProcessIO) (process garden.Process, err error) {
					specsCh <- spec

					defer GinkgoRecover()
					// get rid of race condition caused by write inside the BeforeEach

					processLock.Lock()
					defer processLock.Unlock()

					switch spec.Path {
					case "/action/path":
						return actionProcess, nil
					case filepath.Join(transformer.HealthCheckDstPath, "healthcheck"):
						oldCount := atomic.AddInt64(&healthcheckCallCount, 1)
						switch oldCount {
						case 1:
							startupIOCh <- io
							return startupProcess, nil
						case 2:
							livenessIOCh <- io
							return livenessProcess, nil
						}
					case "/monitor/path":
						return monitorProcess, nil
					}

					err = errors.New("")
					Fail("unexpected executable path: " + spec.Path)
					return
				}
				container = executor.Container{
					RunInfo: executor.RunInfo{
						Action: &models.Action{
							RunAction: &models.RunAction{
								Path: "/action/path",
							},
						},
						Monitor: &models.Action{
							RunAction: &models.RunAction{
								Path: "/monitor/path",
							},
						},
						CheckDefinition: nil, // populated by the other BeforeEaches as necessary
					},
				}
			})

			JustBeforeEach(func() {
				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
				Expect(err).NotTo(HaveOccurred())

				process = ifrit.Background(runner)
			})

			AfterEach(func() {
				close(startupCh)
				livenessCh <- 1 // the healthcheck in liveness mode can only exit by failing
				close(actionCh)
				close(monitorCh)
				ginkgomon.Interrupt(process)
			})

			Context("when declarative healthchecks are enabled", func() {
				BeforeEach(func() {
					options = append(options, transformer.WithDeclarativeHealthchecks())

					container.StartTimeoutMs = 1000
				})

				AfterEach(func() {
					process.Signal(os.Kill)
				})

				Context("and no check definitions exist", func() {
					JustBeforeEach(func() {
						clock.WaitForWatcherAndIncrement(unhealthyMonitoringInterval)
					})

					It("uses the monitor action", func() {
						Eventually(gardenContainer.RunCallCount, 5*time.Second).Should(Equal(2))
						paths := []string{}
						for i := 0; i < gardenContainer.RunCallCount(); i++ {
							spec, _ := gardenContainer.RunArgsForCall(i)
							paths = append(paths, spec.Path)
						}

						Expect(paths).To(ContainElement("/monitor/path"))
					})

					Context("and container proxy is enabled", func() {
						BeforeEach(func() {
							options = append(options, transformer.WithContainerProxy(time.Second))
							cfg.BindMounts = append(cfg.BindMounts, garden.BindMount{
								Origin:  garden.BindMountOriginHost,
								SrcPath: declarativeHealthcheckSrcPath,
								DstPath: transformer.HealthCheckDstPath,
							})
							cfg.ProxyTLSPorts = []uint16{61001}
						})

						It("runs healthchecks for the envoy proxy ports", func() {
							Eventually(specs).Should(Receive(Equal(garden.ProcessSpec{
								ID:   fmt.Sprintf("%s-%s", gardenContainer.Handle(), "envoy-startup-healthcheck-0"),
								Path: filepath.Join(transformer.HealthCheckDstPath, "healthcheck"),
								Args: []string{
									"-port=61001",
									"-timeout=1000ms",
									fmt.Sprintf("-startup-interval=%s", unhealthyMonitoringInterval),
									fmt.Sprintf("-startup-timeout=%s", 1000*time.Millisecond),
								},
								Env: []string{
									"CF_INSTANCE_IP=",
									"CF_INSTANCE_INTERNAL_IP=",
									"CF_INSTANCE_PORT=",
									"CF_INSTANCE_ADDR=",
									"CF_INSTANCE_PORTS=[]",
								},
								Limits: garden.ResourceLimits{
									Nofile: proto.Uint64(1024),
								},
								OverrideContainerLimits: &garden.ProcessLimits{},
								Image:                   garden.ImageRef{URI: "preloaded:cflinuxfs3"},
								BindMounts: []garden.BindMount{
									{
										SrcPath: "/some/source",
										DstPath: "/some/destintation",
										Mode:    garden.BindMountModeRO,
										Origin:  garden.BindMountOriginHost,
									},
									{
										Origin:  garden.BindMountOriginHost,
										SrcPath: declarativeHealthcheckSrcPath,
										DstPath: transformer.HealthCheckDstPath,
									},
								},
							})))
						})
					})
				})

				Context("and an http check definition exists", func() {
					BeforeEach(func() {
						cfg.BindMounts = append(cfg.BindMounts, garden.BindMount{
							Origin:  garden.BindMountOriginHost,
							SrcPath: declarativeHealthcheckSrcPath,
							DstPath: transformer.HealthCheckDstPath,
						})
						container.CheckDefinition = &models.CheckDefinition{
							Checks: []*models.Check{
								&models.Check{
									HttpCheck: &models.HTTPCheck{
										Port:             5432,
										RequestTimeoutMs: 100,
										Path:             "/some/path",
										IntervalMs:       427,
									},
								},
							},
						}
					})

					Context("and container proxy is enabled", func() {
						BeforeEach(func() {
							options = append(options, transformer.WithContainerProxy(time.Second))
							cfg.ProxyTLSPorts = []uint16{61001}
						})

						It("runs a startup healthcheck for the envoy proxy ports", func() {
							Eventually(specs).Should(Receive(Equal(garden.ProcessSpec{
								ID:   fmt.Sprintf("%s-%s", gardenContainer.Handle(), "envoy-startup-healthcheck-0"),
								Path: filepath.Join(transformer.HealthCheckDstPath, "healthcheck"),
								Args: []string{
									"-port=61001",
									"-timeout=1000ms",
									fmt.Sprintf("-startup-interval=%s", unhealthyMonitoringInterval),
									fmt.Sprintf("-startup-timeout=%s", 1000*time.Millisecond),
								},
								Env: []string{
									"CF_INSTANCE_IP=",
									"CF_INSTANCE_INTERNAL_IP=",
									"CF_INSTANCE_PORT=",
									"CF_INSTANCE_ADDR=",
									"CF_INSTANCE_PORTS=[]",
								},
								Limits: garden.ResourceLimits{
									Nofile: proto.Uint64(1024),
								},
								OverrideContainerLimits: &garden.ProcessLimits{},
								Image:                   garden.ImageRef{URI: "preloaded:cflinuxfs3"},
								BindMounts: []garden.BindMount{
									{
										SrcPath: "/some/source",
										DstPath: "/some/destintation",
										Mode:    garden.BindMountModeRO,
										Origin:  garden.BindMountOriginHost,
									},
									{
										Origin:  garden.BindMountOriginHost,
										SrcPath: declarativeHealthcheckSrcPath,
										DstPath: transformer.HealthCheckDstPath,
									},
								},
							})))
						})
					})

					It("runs the startup healthcheck in a sidecar container", func() {
						Eventually(specs).Should(Receive(Equal(garden.ProcessSpec{
							ID:   fmt.Sprintf("%s-%s", gardenContainer.Handle(), "startup-healthcheck-0"),
							Path: filepath.Join(transformer.HealthCheckDstPath, "healthcheck"),
							Args: []string{
								"-port=5432",
								"-timeout=100ms",
								"-uri=/some/path",
								fmt.Sprintf("-startup-interval=%s", unhealthyMonitoringInterval),
								fmt.Sprintf("-startup-timeout=%s", 1000*time.Millisecond),
							},
							Env: []string{
								"CF_INSTANCE_IP=",
								"CF_INSTANCE_INTERNAL_IP=",
								"CF_INSTANCE_PORT=",
								"CF_INSTANCE_ADDR=",
								"CF_INSTANCE_PORTS=[]",
							},
							Limits: garden.ResourceLimits{
								Nofile: proto.Uint64(1024),
							},
							OverrideContainerLimits: &garden.ProcessLimits{},
							Image:                   garden.ImageRef{URI: "preloaded:cflinuxfs3"},
							BindMounts: []garden.BindMount{
								{
									SrcPath: "/some/source",
									DstPath: "/some/destintation",
									Mode:    garden.BindMountModeRO,
									Origin:  garden.BindMountOriginHost,
								},
								{
									Origin:  garden.BindMountOriginHost,
									SrcPath: declarativeHealthcheckSrcPath,
									DstPath: transformer.HealthCheckDstPath,
								},
							},
						})))
					})

					Context("when the container is privileged", func() {
						BeforeEach(func() {
							container.Privileged = true
						})

						It("runs the startup healthcheck in a sidecar container", func() {
							Eventually(specs).Should(Receive(Equal(garden.ProcessSpec{
								ID:   fmt.Sprintf("%s-%s", gardenContainer.Handle(), "startup-healthcheck-0"),
								Path: filepath.Join(transformer.HealthCheckDstPath, "healthcheck"),
								Args: []string{
									"-port=5432",
									"-timeout=100ms",
									"-uri=/some/path",
									fmt.Sprintf("-startup-interval=%s", unhealthyMonitoringInterval),
									fmt.Sprintf("-startup-timeout=%s", 1000*time.Millisecond),
								},
								Env: []string{
									"CF_INSTANCE_IP=",
									"CF_INSTANCE_INTERNAL_IP=",
									"CF_INSTANCE_PORT=",
									"CF_INSTANCE_ADDR=",
									"CF_INSTANCE_PORTS=[]",
								},
								Limits: garden.ResourceLimits{
									Nofile: proto.Uint64(1024),
								},
								OverrideContainerLimits: &garden.ProcessLimits{},
								Image:                   garden.ImageRef{URI: "preloaded:cflinuxfs3"},
								BindMounts: []garden.BindMount{
									{
										SrcPath: "/some/source",
										DstPath: "/some/destintation",
										Mode:    garden.BindMountModeRO,
										Origin:  garden.BindMountOriginHost,
									},
									{
										Origin:  garden.BindMountOriginHost,
										SrcPath: declarativeHealthcheckSrcPath,
										DstPath: transformer.HealthCheckDstPath,
									},
								},
							})))
						})
					})

					Context("and the starttimeout is set to 0", func() {
						BeforeEach(func() {
							container.StartTimeoutMs = 0
						})

						It("runs the healthcheck with startup timeout set to 0", func() {
							Eventually(gardenContainer.RunCallCount).Should(Equal(2))
							paths := []string{}
							args := [][]string{}
							for i := 0; i < gardenContainer.RunCallCount(); i++ {
								spec, _ := gardenContainer.RunArgsForCall(i)
								paths = append(paths, spec.Path)
								args = append(args, spec.Args)
							}

							Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
							Expect(args).To(ContainElement([]string{
								"-port=5432",
								"-timeout=100ms",
								"-uri=/some/path",
								"-startup-interval=1ms",
								"-startup-timeout=0s",
							}))
						})
					})

					Context("and optional fields are missing", func() {
						BeforeEach(func() {
							container.CheckDefinition = &models.CheckDefinition{
								Checks: []*models.Check{
									&models.Check{
										HttpCheck: &models.HTTPCheck{
											Port: 6432,
										},
									},
								},
							}
						})

						It("uses sane defaults", func() {
							Eventually(gardenContainer.RunCallCount).Should(Equal(2))
							paths := []string{}
							args := [][]string{}
							for i := 0; i < gardenContainer.RunCallCount(); i++ {
								spec, _ := gardenContainer.RunArgsForCall(i)
								paths = append(paths, spec.Path)
								args = append(args, spec.Args)
							}

							Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
							Expect(args).To(ContainElement([]string{
								"-port=6432",
								"-timeout=1000ms",
								"-uri=/",
								"-startup-interval=1ms",
								"-startup-timeout=1s",
							}))
						})
					})

					It("uses the check definition", func() {
						Eventually(gardenContainer.RunCallCount).Should(Equal(2))
						paths := []string{}
						args := [][]string{}
						for i := 0; i < gardenContainer.RunCallCount(); i++ {
							spec, _ := gardenContainer.RunArgsForCall(i)
							paths = append(paths, spec.Path)
							args = append(args, spec.Args)
						}

						Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
						Expect(args).To(ContainElement([]string{
							"-port=5432",
							"-timeout=100ms",
							"-uri=/some/path",
							"-startup-interval=1ms",
							"-startup-timeout=1s",
						}))
					})

					Context("when the startup check times out", func() {
						JustBeforeEach(func() {
							By("waiting for the action and startup check processes to start")
							var io garden.ProcessIO
							Eventually(startupIO).Should(Receive(&io))
							_, err := io.Stdout.Write([]byte("startup check starting\n"))
							_, err = io.Stderr.Write([]byte("startup check failed\n"))
							Expect(err).NotTo(HaveOccurred())

							By("timing out the startup check")
							Eventually(gardenContainer.RunCallCount).Should(Equal(2))

							Consistently(startupProcess.SignalCallCount).Should(Equal(0))
							startupCh <- 1
							Eventually(actionProcess.SignalCallCount).Should(Equal(1))
							actionCh <- 2
						})

						It("suppress the startup check log", func() {
							Eventually(process.Wait()).Should(Receive(HaveOccurred()))
							Consistently(fakeMetronClient.SendAppLogCallCount).Should(Equal(2))
							msg0, _, _ := fakeMetronClient.SendAppLogArgsForCall(0)
							msg1, _, _ := fakeMetronClient.SendAppLogArgsForCall(1)
							Expect([]string{msg0, msg1}).To(ConsistOf("Starting health monitoring of container", "Exit status 2"))
						})

						It("logs the startup check output on stderr", func() {
							Eventually(fakeMetronClient.SendAppErrorLogCallCount).Should(Equal(3))
							logLines := map[string]string{}
							msg, source, _ := fakeMetronClient.SendAppErrorLogArgsForCall(0)
							logLines[source] = msg
							Expect(logLines["HEALTH"]).To(Equal("startup check starting"))
							msg, source, _ = fakeMetronClient.SendAppErrorLogArgsForCall(1)
							logLines[source] = msg
							Expect(logLines["HEALTH"]).To(Equal("startup check failed"))
							msg, source, _ = fakeMetronClient.SendAppErrorLogArgsForCall(2)
							logLines[source] = msg
							Expect(logLines["test"]).To(MatchRegexp("Failed after 1\\d\\dms: startup health check never passed."))
						})

						It("returns the startup check output in the error", func() {
							Consistently(process.Ready()).ShouldNot(BeClosed())
						})

						It("returns the startup check output in the error", func() {
							Eventually(process.Wait()).Should(Receive(MatchError(MatchRegexp("Instance never healthy after 1\\d\\dms: startup check starting\nstartup check failed"))))
						})
					})

					Context("when the startup check passes", func() {
						JustBeforeEach(func() {
							startupCh <- 0
						})

						It("starts the liveness check", func() {
							Eventually(gardenContainer.RunCallCount).Should(Equal(3))
							ids := []string{}
							paths := []string{}
							args := [][]string{}
							for i := 0; i < gardenContainer.RunCallCount(); i++ {
								spec, _ := gardenContainer.RunArgsForCall(i)
								ids = append(ids, spec.ID)
								paths = append(paths, spec.Path)
								args = append(args, spec.Args)
							}

							Expect(ids).To(ContainElement(fmt.Sprintf("%s-%s", gardenContainer.Handle(), "liveness-healthcheck-0")))
							Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
							Expect(args).To(ContainElement([]string{
								"-port=5432",
								"-timeout=100ms",
								"-uri=/some/path",
								"-liveness-interval=427ms",
							}))
						})

						Context("when optional values are not provided in liveness check defintion", func() {
							BeforeEach(func() {
								container.CheckDefinition = &models.CheckDefinition{
									Checks: []*models.Check{
										&models.Check{
											HttpCheck: &models.HTTPCheck{
												Port: 6432,
											},
										},
									},
								}
							})

							It("starts the liveness check with sane defaults", func() {
								Eventually(gardenContainer.RunCallCount).Should(Equal(3))
								ids := []string{}
								paths := []string{}
								args := [][]string{}
								for i := 0; i < gardenContainer.RunCallCount(); i++ {
									spec, _ := gardenContainer.RunArgsForCall(i)
									ids = append(ids, spec.ID)
									paths = append(paths, spec.Path)
									args = append(args, spec.Args)
								}

								Expect(ids).To(ContainElement(fmt.Sprintf("%s-%s", gardenContainer.Handle(), "liveness-healthcheck-0")))
								Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
								Expect(args).To(ContainElement([]string{
									"-port=6432",
									"-timeout=1000ms",
									"-uri=/",
									"-liveness-interval=1s",
								}))
							})

						})

						Context("when the liveness check exits", func() {
							JustBeforeEach(func() {
								Eventually(gardenContainer.RunCallCount).Should(Equal(3))

								By("waiting the action and liveness check processes to start")
								var io garden.ProcessIO
								Eventually(livenessIO).Should(Receive(&io))
								_, err := io.Stdout.Write([]byte("liveness check failed"))
								Expect(err).NotTo(HaveOccurred())

								By("exiting the liveness check")
								livenessCh <- 1
								Eventually(actionProcess.SignalCallCount).Should(Equal(1))
								actionCh <- 2
							})

							It("logs the liveness check output on stderr", func() {
								Eventually(fakeMetronClient.SendAppErrorLogCallCount).Should(Equal(2))
								logLines := map[string]string{}
								msg, source, _ := fakeMetronClient.SendAppErrorLogArgsForCall(0)
								logLines[source] = msg
								Expect(logLines).To(Equal(map[string]string{
									"HEALTH": "liveness check failed",
								}))
								msg, source, _ = fakeMetronClient.SendAppErrorLogArgsForCall(1)
								logLines[source] = msg
								Expect(logLines).To(Equal(map[string]string{
									"HEALTH": "liveness check failed",
									"test":   "Container became unhealthy",
								}))
							})

							It("returns the liveness check output in the error", func() {
								Eventually(process.Wait()).Should(Receive(MatchError(ContainSubstring("Instance became unhealthy: liveness check failed"))))
							})
						})
					})
				})

				Context("and a tcp check definition exists", func() {
					BeforeEach(func() {
						container.CheckDefinition = &models.CheckDefinition{
							Checks: []*models.Check{
								&models.Check{
									TcpCheck: &models.TCPCheck{
										Port:             5432,
										ConnectTimeoutMs: 100,
										IntervalMs:       44,
									},
								},
							},
						}
					})

					Context("and optional fields are missing", func() {
						BeforeEach(func() {
							container.CheckDefinition = &models.CheckDefinition{
								Checks: []*models.Check{
									&models.Check{
										TcpCheck: &models.TCPCheck{
											Port: 6432,
										},
									},
								},
							}
						})

						It("uses sane defaults", func() {
							Eventually(gardenContainer.RunCallCount).Should(Equal(2))
							paths := []string{}
							args := [][]string{}
							for i := 0; i < gardenContainer.RunCallCount(); i++ {
								spec, _ := gardenContainer.RunArgsForCall(i)
								paths = append(paths, spec.Path)
								args = append(args, spec.Args)
							}

							Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
							Expect(args).To(ContainElement([]string{
								"-port=6432",
								"-timeout=1000ms",
								"-startup-interval=1ms",
								"-startup-timeout=1s",
							}))
						})
					})

					It("uses the startup check definition", func() {
						Eventually(gardenContainer.RunCallCount).Should(Equal(2))
						ids := []string{}
						paths := []string{}
						args := [][]string{}
						for i := 0; i < gardenContainer.RunCallCount(); i++ {
							spec, _ := gardenContainer.RunArgsForCall(i)
							ids = append(ids, spec.ID)
							paths = append(paths, spec.Path)
							args = append(args, spec.Args)
						}

						Expect(ids).To(ContainElement(fmt.Sprintf("%s-%s", gardenContainer.Handle(), "startup-healthcheck-0")))
						Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
						Expect(args).To(ContainElement([]string{
							"-port=5432",
							"-timeout=100ms",
							"-startup-interval=1ms",
							"-startup-timeout=1s",
						}))
					})

					Context("when the startup check passes", func() {
						JustBeforeEach(func() {
							startupCh <- 0
						})

						It("uses the liveness check definition", func() {
							Eventually(gardenContainer.RunCallCount).Should(Equal(3))
							ids := []string{}
							paths := []string{}
							args := [][]string{}
							for i := 0; i < gardenContainer.RunCallCount(); i++ {
								spec, _ := gardenContainer.RunArgsForCall(i)
								ids = append(ids, spec.ID)
								paths = append(paths, spec.Path)
								args = append(args, spec.Args)
							}

							Expect(ids).To(ContainElement(fmt.Sprintf("%s-%s", gardenContainer.Handle(), "liveness-healthcheck-0")))
							Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
							Expect(args).To(ContainElement([]string{
								"-port=5432",
								"-timeout=100ms",
								"-liveness-interval=44ms",
							}))

						})

						Context("and optional fields are missing", func() {
							BeforeEach(func() {
								container.CheckDefinition = &models.CheckDefinition{
									Checks: []*models.Check{
										&models.Check{
											TcpCheck: &models.TCPCheck{
												Port: 6432,
											},
										},
									},
								}
							})

							It("uses sane defaults", func() {
								Eventually(gardenContainer.RunCallCount).Should(Equal(3))
								ids := []string{}
								paths := []string{}
								args := [][]string{}
								for i := 0; i < gardenContainer.RunCallCount(); i++ {
									spec, _ := gardenContainer.RunArgsForCall(i)
									ids = append(ids, spec.ID)
									paths = append(paths, spec.Path)
									args = append(args, spec.Args)
								}

								Expect(ids).To(ContainElement(fmt.Sprintf("%s-%s", gardenContainer.Handle(), "liveness-healthcheck-0")))
								Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
								Expect(args).To(ContainElement([]string{
									"-port=6432",
									"-timeout=1000ms",
									"-liveness-interval=1s",
								}))
							})
						})

					})
				})

				Context("logs", func() {
					BeforeEach(func() {
						container.CheckDefinition = &models.CheckDefinition{
							Checks: []*models.Check{
								&models.Check{
									HttpCheck: &models.HTTPCheck{
										Port:             5432,
										RequestTimeoutMs: 2000,
										Path:             "/some/path",
									},
								},
							},
						}
					})

					JustBeforeEach(func() {
						Eventually(fakeMetronClient.SendAppLogCallCount, 5).Should(BeNumerically(">=", 1))

						message, sourceName, _ := fakeMetronClient.SendAppLogArgsForCall(0)
						Expect(message).To(Equal("Starting health monitoring of container"))
						Expect(sourceName).To(Equal("test"))

						var io garden.ProcessIO
						Eventually(startupIO).Should(Receive(&io))
						io.Stdout.Write([]byte("failed"))

						startupCh <- 1
					})

					It("should default to HEALTH for log source", func() {
						Eventually(fakeMetronClient.SendAppErrorLogCallCount).Should(BeNumerically(">=", 1))
						_, sourceName, _ := fakeMetronClient.SendAppErrorLogArgsForCall(0)
						Expect(sourceName).To(Equal("HEALTH"))
					})

					Context("when log source defined", func() {
						BeforeEach(func() {
							container.CheckDefinition.LogSource = "healthcheck"
						})

						It("logs healthcheck errors with log source from check defintion", func() {
							Eventually(fakeMetronClient.SendAppErrorLogCallCount).Should(BeNumerically(">=", 1))
							_, sourceName, _ := fakeMetronClient.SendAppErrorLogArgsForCall(0)
							Expect(sourceName).To(Equal("healthcheck"))
						})
					})
				})

				Context("and multiple check definitions exists", func() {
					var (
						otherStartupProcess  *gardenfakes.FakeProcess
						otherStartupCh       chan int
						otherLivenessProcess *gardenfakes.FakeProcess
						otherLivenessCh      chan int
					)

					BeforeEach(func() {
						// get rid of race condition caused by read inside the RunStub
						processLock.Lock()
						defer processLock.Unlock()

						otherStartupCh = make(chan int)
						otherStartupProcess = makeProcess(otherStartupCh)

						otherLivenessCh = make(chan int)
						otherLivenessProcess = makeProcess(otherLivenessCh)

						healthcheckCallCount := int64(0)
						gardenContainer.RunStub = func(spec garden.ProcessSpec, io garden.ProcessIO) (process garden.Process, err error) {
							defer GinkgoRecover()
							// get rid of race condition caused by write inside the BeforeEach
							processLock.Lock()
							defer processLock.Unlock()

							switch spec.Path {
							case "/action/path":
								return actionProcess, nil
							case filepath.Join(transformer.HealthCheckDstPath, "healthcheck"):
								oldCount := atomic.AddInt64(&healthcheckCallCount, 1)
								switch oldCount {
								case 1:
									return startupProcess, nil
								case 2:
									return otherStartupProcess, nil
								case 3:
									return livenessProcess, nil
								case 4:
									return otherLivenessProcess, nil
								}
								return livenessProcess, nil
							case "/monitor/path":
								return monitorProcess, nil
							}

							err = errors.New("")
							Fail("unexpected executable path: " + spec.Path)
							return
						}

						container.CheckDefinition = &models.CheckDefinition{
							Checks: []*models.Check{
								&models.Check{
									TcpCheck: &models.TCPCheck{
										Port:             2222,
										ConnectTimeoutMs: 100,
										IntervalMs:       50,
									},
								},
								&models.Check{
									HttpCheck: &models.HTTPCheck{
										Port:             8080,
										RequestTimeoutMs: 100,
										IntervalMs:       50,
									},
								},
							},
						}
					})

					AfterEach(func() {
						close(otherStartupCh)
						close(otherLivenessCh)
					})

					It("uses the check definition instead of the monitor action", func() {
						Eventually(gardenContainer.RunCallCount).Should(Equal(3))
						ids := []string{}
						paths := []string{}
						args := [][]string{}
						for i := 0; i < gardenContainer.RunCallCount(); i++ {
							spec, _ := gardenContainer.RunArgsForCall(i)
							ids = append(ids, spec.ID)
							paths = append(paths, spec.Path)
							args = append(args, spec.Args)
						}

						Expect(ids).To(ContainElement(fmt.Sprintf("%s-%s", gardenContainer.Handle(), "startup-healthcheck-0")))
						Expect(ids).To(ContainElement(fmt.Sprintf("%s-%s", gardenContainer.Handle(), "startup-healthcheck-1")))

						Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
						Expect(args).To(ContainElement([]string{
							"-port=2222",
							"-timeout=100ms",
							"-startup-interval=1ms",
							"-startup-timeout=1s",
						}))
						Expect(args).To(ContainElement([]string{
							"-port=8080",
							"-timeout=100ms",
							"-uri=/",
							"-startup-interval=1ms",
							"-startup-timeout=1s",
						}))
					})

					Context("when one of the startup checks finish", func() {
						JustBeforeEach(func() {
							Eventually(gardenContainer.RunCallCount).Should(Equal(3))
							startupCh <- 0
						})

						It("waits for both healthchecks to pass", func() {
							Consistently(gardenContainer.RunCallCount).Should(Equal(3))
						})

						Context("and the other startup check finish", func() {
							JustBeforeEach(func() {
								otherStartupCh <- 0
							})

							It("starts the liveness checks", func() {
								Eventually(gardenContainer.RunCallCount).Should(Equal(5))
								ids := []string{}
								paths := []string{}
								args := [][]string{}
								for i := 0; i < gardenContainer.RunCallCount(); i++ {
									spec, _ := gardenContainer.RunArgsForCall(i)
									ids = append(ids, spec.ID)
									paths = append(paths, spec.Path)
									args = append(args, spec.Args)
								}

								Expect(ids).To(ContainElement(fmt.Sprintf("%s-%s", gardenContainer.Handle(), "liveness-healthcheck-0")))
								Expect(ids).To(ContainElement(fmt.Sprintf("%s-%s", gardenContainer.Handle(), "liveness-healthcheck-1")))

								Expect(paths).To(ContainElement(filepath.Join(transformer.HealthCheckDstPath, "healthcheck")))
								Expect(args).To(ContainElement([]string{
									"-port=2222",
									"-timeout=100ms",
									"-liveness-interval=50ms",
								}))
								Expect(args).To(ContainElement([]string{
									"-port=8080",
									"-timeout=100ms",
									"-uri=/",
									"-liveness-interval=50ms",
								}))
							})

							Context("when either liveness check exit", func() {
								JustBeforeEach(func() {
									Eventually(gardenContainer.RunCallCount).Should(Equal(5))
									livenessCh <- 1
								})

								It("signals the process and exit", func() {
									Eventually(otherLivenessProcess.SignalCallCount).ShouldNot(BeZero())
									otherLivenessCh <- 1

									Eventually(actionProcess.SignalCallCount).ShouldNot(BeZero())
									actionCh <- 0

									Eventually(process.Wait()).Should(Receive(HaveOccurred()))
								})
							})
						})
					})
				})
			})

			Context("when they are disabled", func() {
				It("ignores the check definition and use the MonitorAction", func() {
					clock.WaitForWatcherAndIncrement(unhealthyMonitoringInterval)
					Eventually(gardenContainer.RunCallCount).Should(Equal(2))
					paths := []string{}
					for i := 0; i < gardenContainer.RunCallCount(); i++ {
						spec, _ := gardenContainer.RunArgsForCall(i)
						paths = append(paths, spec.Path)
					}

					Expect(paths).To(ContainElement("/monitor/path"))
				})

				Context("and container proxy is enabled", func() {
					BeforeEach(func() {
						options = append(options, transformer.WithContainerProxy(time.Second))
						cfg.BindMounts = append(cfg.BindMounts, garden.BindMount{
							Origin:  garden.BindMountOriginHost,
							SrcPath: declarativeHealthcheckSrcPath,
							DstPath: transformer.HealthCheckDstPath,
						})
						cfg.ProxyTLSPorts = []uint16{61001}
					})

					It("does not run healthchecks for the envoy proxy ports", func() {
						Consistently(specs).ShouldNot(Receive(Equal(garden.ProcessSpec{
							ID:   fmt.Sprintf("%s-%s", gardenContainer.Handle(), "envoy-startup-healthcheck-0"),
							Path: filepath.Join(transformer.HealthCheckDstPath, "healthcheck"),
							Args: []string{
								"-port=61001",
								"-timeout=1000ms",
								fmt.Sprintf("-startup-interval=%s", unhealthyMonitoringInterval),
								fmt.Sprintf("-startup-timeout=%s", 1000*time.Millisecond),
							},
							Env: []string{},
							Limits: garden.ResourceLimits{
								Nofile: proto.Uint64(1024),
							},
							OverrideContainerLimits: &garden.ProcessLimits{},
							Image:                   garden.ImageRef{URI: "preloaded:cflinuxfs3"},
							BindMounts: []garden.BindMount{
								{
									SrcPath: "/some/source",
									DstPath: "/some/destintation",
									Mode:    garden.BindMountModeRO,
									Origin:  garden.BindMountOriginHost,
								},
								{
									Origin:  garden.BindMountOriginHost,
									SrcPath: declarativeHealthcheckSrcPath,
									DstPath: transformer.HealthCheckDstPath,
								},
							},
						})))
					})
				})

				Context("and there is no monitor action", func() {
					BeforeEach(func() {
						container.Monitor = nil
					})

					It("does not run any healthchecks", func() {
						Eventually(gardenContainer.RunCallCount).Should(Equal(1))
						Consistently(gardenContainer.RunCallCount).Should(Equal(1))

						paths := []string{}
						for i := 0; i < gardenContainer.RunCallCount(); i++ {
							spec, _ := gardenContainer.RunArgsForCall(i)
							paths = append(paths, spec.Path)
						}

						Expect(paths).To(ContainElement("/action/path"))
					})
				})
			})
		})

		Context("when there is no setup", func() {
			BeforeEach(func() {
				container.Setup = nil
			})

			It("returns a codependent step for the action/monitor", func() {
				gardenContainer.RunReturns(&gardenfakes.FakeProcess{}, nil)

				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
				Expect(err).NotTo(HaveOccurred())

				process := ifrit.Background(runner)

				Eventually(gardenContainer.RunCallCount).Should(Equal(1))
				processSpec, _ := gardenContainer.RunArgsForCall(0)
				Expect(processSpec.Path).To(Equal("/action/path"))
				Consistently(gardenContainer.RunCallCount).Should(Equal(1))

				clock.Increment(1 * time.Second)
				Eventually(gardenContainer.RunCallCount).Should(Equal(2))
				processSpec, _ = gardenContainer.RunArgsForCall(1)
				Expect(processSpec.Path).To(Equal("/monitor/path"))
				Eventually(process.Ready()).Should(BeClosed())

				process.Signal(os.Interrupt)
				clock.Increment(1 * time.Second)
				Eventually(process.Wait()).Should(Receive(nil))
			})

			It("logs the container creation time", func() {
				gardenContainer.RunReturns(&gardenfakes.FakeProcess{}, nil)
				cfg.CreationStartTime = clock.Now()
				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
				Expect(err).NotTo(HaveOccurred())
				ifrit.Background(runner)

				Eventually(logger).Should(gbytes.Say("container-setup.*duration.*:0"))
			})
		})

		Context("when there is no monitor", func() {
			BeforeEach(func() {
				container.Monitor = nil
				container.Setup = nil
			})

			It("does not run the monitor step and immediately says the healthcheck passed", func() {
				blockCh := make(chan struct{})
				defer close(blockCh)

				fakeGardenProcess := &gardenfakes.FakeProcess{}
				fakeGardenProcess.WaitStub = func() (int, error) {
					<-blockCh
					return 0, nil
				}
				gardenContainer.RunReturns(fakeGardenProcess, nil)

				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
				Expect(err).NotTo(HaveOccurred())

				process := ifrit.Background(runner)
				Eventually(process.Ready()).Should(BeClosed())

				Eventually(gardenContainer.RunCallCount).Should(Equal(1))
				processSpec, _ := gardenContainer.RunArgsForCall(0)
				Expect(processSpec.Path).To(Equal("/action/path"))
				Consistently(gardenContainer.RunCallCount).Should(Equal(1))
			})
		})

		Context("MonitorAction", func() {
			var (
				process ifrit.Process
			)

			JustBeforeEach(func() {
				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer, cfg)
				Expect(err).NotTo(HaveOccurred())
				process = ifrit.Background(runner)
			})

			AfterEach(func() {
				ginkgomon.Interrupt(process)
			})

			BeforeEach(func() {
				container.Setup = nil
				container.Monitor = &models.Action{
					ParallelAction: models.Parallel(
						&models.RunAction{
							Path:              "/monitor/path",
							SuppressLogOutput: true,
						},
						&models.RunAction{
							Path:              "/monitor/path",
							SuppressLogOutput: true,
						},
					),
				}
			})

			Context("SuppressLogOutput", func() {
				var (
					monitorCh, actionCh chan int
				)

				BeforeEach(func() {
					monitorCh = make(chan int, 2)
					actionCh = make(chan int, 1)

					gardenContainer.RunStub = func(processSpec garden.ProcessSpec, processIO garden.ProcessIO) (garden.Process, error) {
						switch processSpec.Path {
						case "/monitor/path":
							return makeProcess(monitorCh), nil
						case "/action/path":
							return makeProcess(actionCh), nil
						default:
							return &gardenfakes.FakeProcess{}, nil
						}
					}
				})

				AfterEach(func() {
					close(monitorCh)
					close(actionCh)
				})

				JustBeforeEach(func() {
					Eventually(gardenContainer.RunCallCount).Should(Equal(1))
					clock.Increment(1 * time.Second)
					Eventually(gardenContainer.RunCallCount).Should(Equal(3))
				})

				It("is ignored", func() {
					processSpec, processIO := gardenContainer.RunArgsForCall(1)
					Expect(processSpec.Path).To(Equal("/monitor/path"))
					Expect(container.Monitor.RunAction.GetSuppressLogOutput()).Should(BeFalse())
					Expect(processIO.Stdout).ShouldNot(Equal(ioutil.Discard))
					Expect(processIO.Stderr).ShouldNot(Equal(ioutil.Discard))
					monitorCh <- 0
					monitorCh <- 0
					Eventually(process.Ready()).Should(BeClosed())
				})
			})

			Context("logs", func() {
				var (
					exitStatusCh    chan int
					monitorProcess1 *gardenfakes.FakeProcess
					monitorProcess2 *gardenfakes.FakeProcess
				)

				BeforeEach(func() {
					monitorProcess1 = &gardenfakes.FakeProcess{}
					monitorProcess2 = &gardenfakes.FakeProcess{}
					actionProcess := &gardenfakes.FakeProcess{}
					exitStatusCh = make(chan int)
					actionProcess.WaitStub = func() (int, error) {
						return <-exitStatusCh, nil
					}

					monitorProcessChan1 := make(chan *garden.ProcessIO, 4)
					monitorProcessChan2 := make(chan *garden.ProcessIO, 4)

					monitorProcess1.WaitStub = func() (int, error) {
						procIO := <-monitorProcessChan1

						if monitorProcess1.WaitCallCount() == 2 {
							procIO.Stdout.Write([]byte("healthcheck failed"))
							return 1, nil
						}
						return 0, nil
					}

					monitorProcess2.WaitStub = func() (int, error) {
						procIO := <-monitorProcessChan2

						if monitorProcess2.WaitCallCount() == 2 {
							procIO.Stdout.Write([]byte("healthcheck failed"))
							return 1, nil
						}
						return 0, nil
					}

					monitorProcessRun := uint32(0)

					gardenContainer.RunStub = func(processSpec garden.ProcessSpec, processIO garden.ProcessIO) (garden.Process, error) {
						if processSpec.Path == "/monitor/path" {
							if atomic.AddUint32(&monitorProcessRun, 1)%2 == 0 {
								monitorProcessChan1 <- &processIO
								return monitorProcess1, nil
							}
							monitorProcessChan2 <- &processIO
							return monitorProcess2, nil
						} else if processSpec.Path == "/action/path" {
							return actionProcess, nil
						}
						return &gardenfakes.FakeProcess{}, nil
					}
				})

				AfterEach(func() {
					Eventually(exitStatusCh).Should(BeSent(1))
				})

				JustBeforeEach(func() {
					Eventually(gardenContainer.RunCallCount).Should(Equal(1))

					By("starting the startup check")
					clock.WaitForWatcherAndIncrement(1 * time.Second)
					Eventually(gardenContainer.RunCallCount).Should(Equal(3))
					Eventually(monitorProcess1.WaitCallCount).Should(Equal(1))
					Eventually(monitorProcess2.WaitCallCount).Should(Equal(1))

					By("starting the liveness check")
					clock.WaitForWatcherAndIncrement(1 * time.Second)
					Eventually(gardenContainer.RunCallCount).Should(Equal(5))
					Eventually(monitorProcess1.WaitCallCount).Should(Equal(2))
					Eventually(monitorProcess2.WaitCallCount).Should(Equal(2))
				})

				It("logs healthcheck error with the same source in a readable way", func() {
					Eventually(fakeMetronClient.SendAppErrorLogCallCount).Should(Equal(2))
					message, sourceName, _ := fakeMetronClient.SendAppErrorLogArgsForCall(0)
					Expect(sourceName).To(Equal("test"))
					Expect(message).To(ContainSubstring("healthcheck failed; healthcheck failed"))
				})

				It("logs the container lifecycle", func() {
					Eventually(fakeMetronClient.SendAppLogCallCount).Should(Equal(2))
					message, _, _ := fakeMetronClient.SendAppLogArgsForCall(0)
					Expect(message).To(Equal("Starting health monitoring of container"))
					message, _, _ = fakeMetronClient.SendAppLogArgsForCall(1)
					Expect(message).To(Equal("Container became healthy"))
					Eventually(fakeMetronClient.SendAppErrorLogCallCount()).Should(Equal(2))
					message, _, _ = fakeMetronClient.SendAppErrorLogArgsForCall(1)
					Expect(message).To(Equal("Container became unhealthy"))
				})
			})
		})
	})
})
