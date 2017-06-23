package transformer_test

import (
	"errors"
	"io/ioutil"
	"os"
	"sync/atomic"
	"time"

	"code.cloudfoundry.org/bbs/models"
	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/log_streamer"
	"code.cloudfoundry.org/executor/depot/transformer"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/garden/gardenfakes"
	mfakes "code.cloudfoundry.org/go-loggregator/fakes"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	"code.cloudfoundry.org/workpool"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
	"github.com/tedsuo/ifrit/ginkgomon"
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
			fakeMetronClient            *mfakes.FakeClient
			healthyMonitoringInterval   time.Duration
			unhealthyMonitoringInterval time.Duration
			healthCheckWorkPool         *workpool.WorkPool
		)

		BeforeEach(func() {
			gardenContainer = &gardenfakes.FakeContainer{}

			logger = lagertest.NewTestLogger("test-container-store")
			fakeMetronClient = &mfakes.FakeClient{}
			logStreamer = log_streamer.New("test", "test", 1, fakeMetronClient)

			healthyMonitoringInterval = 1 * time.Second
			unhealthyMonitoringInterval = 1 * time.Millisecond

			var err error
			healthCheckWorkPool, err = workpool.NewWorkPool(10)
			Expect(err).NotTo(HaveOccurred())

			clock = fakeclock.NewFakeClock(time.Now())

			optimusPrime = transformer.NewTransformer(
				nil, nil, nil, nil, nil, nil,
				os.TempDir(),
				false,
				healthyMonitoringInterval,
				unhealthyMonitoringInterval,
				healthCheckWorkPool,
				clock,
				[]string{"/post-setup/path", "-x", "argument"},
				"jim",
				false,
			)

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

		Context("when there is no run action", func() {
			BeforeEach(func() {
				container.Action = nil
			})

			It("returns an error", func() {
				_, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer)
				Expect(err).To(HaveOccurred())
			})
		})

		It("returns a step encapsulating setup, post-setup, monitor, and action", func() {
			setupReceived := make(chan struct{})
			postSetupReceived := make(chan struct{})
			monitorProcess := &gardenfakes.FakeProcess{}
			gardenContainer.RunStub = func(processSpec garden.ProcessSpec, processIO garden.ProcessIO) (garden.Process, error) {
				if processSpec.Path == "/setup/path" {
					setupReceived <- struct{}{}
				} else if processSpec.Path == "/post-setup/path" {
					postSetupReceived <- struct{}{}
				} else if processSpec.Path == "/monitor/path" {
					return monitorProcess, nil
				}
				return &gardenfakes.FakeProcess{}, nil
			}

			monitorProcess.WaitStub = func() (int, error) {
				if monitorProcess.WaitCallCount() == 1 {
					return 1, errors.New("boom")
				} else {
					return 0, nil
				}
			}

			runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer)
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

			Eventually(gardenContainer.RunCallCount).Should(Equal(3))
			processSpec, _ = gardenContainer.RunArgsForCall(2)
			Expect(processSpec.Path).To(Equal("/action/path"))
			Consistently(gardenContainer.RunCallCount).Should(Equal(3))

			Consistently(process.Ready()).ShouldNot(Receive())

			clock.Increment(1 * time.Second)
			Eventually(gardenContainer.RunCallCount).Should(Equal(4))
			processSpec, _ = gardenContainer.RunArgsForCall(3)
			Expect(processSpec.Path).To(Equal("/monitor/path"))
			Consistently(process.Ready()).ShouldNot(Receive())

			clock.Increment(1 * time.Second)
			Eventually(gardenContainer.RunCallCount).Should(Equal(5))
			processSpec, processIO := gardenContainer.RunArgsForCall(4)
			Expect(processSpec.Path).To(Equal("/monitor/path"))
			Expect(container.Monitor.RunAction.GetSuppressLogOutput()).Should(BeFalse())
			Expect(processIO.Stdout).ShouldNot(Equal(ioutil.Discard))
			Eventually(process.Ready()).Should(BeClosed())

			process.Signal(os.Interrupt)
			clock.Increment(1 * time.Second)
			Eventually(process.Wait()).Should(Receive(nil))
		})

		Describe("declarative healthchecks", func() {
			var (
				process          ifrit.Process
				readinessProcess *gardenfakes.FakeProcess
				readinessCh      chan int
				livenessProcess  *gardenfakes.FakeProcess
				livenessCh       chan int
				actionProcess    *gardenfakes.FakeProcess
				actionCh         chan int
				monitorProcess   *gardenfakes.FakeProcess
				monitorCh        chan int
			)

			makeProcess := func(waitCh chan int) *gardenfakes.FakeProcess {
				process := &gardenfakes.FakeProcess{}
				process.WaitStub = func() (int, error) {
					return <-waitCh, nil
				}
				return process
			}

			BeforeEach(func() {
				readinessCh = make(chan int)
				readinessProcess = makeProcess(readinessCh)

				livenessCh = make(chan int)
				livenessProcess = makeProcess(livenessCh)

				actionCh = make(chan int)
				actionProcess = makeProcess(actionCh)

				monitorCh = make(chan int)
				monitorProcess = makeProcess(monitorCh)

				healthcheckCallCount := int64(0)
				gardenContainer.RunStub = func(spec garden.ProcessSpec, io garden.ProcessIO) (process garden.Process, err error) {
					defer GinkgoRecover()

					switch spec.Path {
					case "/action/path":
						return actionProcess, nil
					case "/etc/cf-assets/healthcheck/healthcheck":
						oldCount := atomic.AddInt64(&healthcheckCallCount, 1)
						switch oldCount {
						case 1:
							return readinessProcess, nil
						case 2:
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
					},
				}
			})

			JustBeforeEach(func() {
				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer)
				Expect(err).NotTo(HaveOccurred())

				process = ifrit.Background(runner)
			})

			AfterEach(func() {
				close(readinessCh)
				close(livenessCh)
				close(actionCh)
				close(monitorCh)
				ginkgomon.Interrupt(process)
			})

			Context("when they are enabled", func() {
				BeforeEach(func() {
					optimusPrime = transformer.NewTransformer(
						nil, nil, nil, nil, nil, nil,
						os.TempDir(),
						false,
						healthyMonitoringInterval,
						unhealthyMonitoringInterval,
						healthCheckWorkPool,
						clock,
						[]string{"/post-setup/path", "-x", "argument"},
						"jim",
						true,
					)

					container.StartTimeoutMs = 1000
				})

				AfterEach(func() {
					process.Signal(os.Kill)
				})

				Context("and no check definitions exist", func() {
					BeforeEach(func() {
						container.CheckDefinition = nil
					})

					JustBeforeEach(func() {
						clock.WaitForWatcherAndIncrement(unhealthyMonitoringInterval)
					})

					It("uses the monitor action", func() {
						Eventually(gardenContainer.RunCallCount, 5*time.Second).Should(Equal(2))
						paths := []string{}
						args := [][]string{}
						for i := 0; i < gardenContainer.RunCallCount(); i++ {
							spec, _ := gardenContainer.RunArgsForCall(i)
							paths = append(paths, spec.Path)
							args = append(args, spec.Args)
						}

						Expect(paths).To(ContainElement("/monitor/path"))
					})
				})

				Context("and an http check definition exists", func() {
					BeforeEach(func() {
						container.CheckDefinition = &models.CheckDefinition{
							Checks: []*models.Check{
								&models.Check{
									HttpCheck: &models.HTTPCheck{
										Port:             5432,
										RequestTimeoutMs: 100,
										Path:             "/some/path",
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
										HttpCheck: &models.HTTPCheck{
											Port: 5432,
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

							Expect(paths).To(ContainElement("/etc/cf-assets/healthcheck/healthcheck"))
							Expect(args).To(ContainElement([]string{
								"-port=5432",
								"-timeout=1000ms",
								"-uri=/",
								"-readiness-interval=1ms", // 100ms
							}))
						})
					})

					It("uses the check definition", func() {
						Eventually(gardenContainer.RunCallCount).Should(Equal(2))
						paths := []string{}
						args := [][]string{}
						users := []string{}
						for i := 0; i < gardenContainer.RunCallCount(); i++ {
							spec, _ := gardenContainer.RunArgsForCall(i)
							paths = append(paths, spec.Path)
							args = append(args, spec.Args)
							users = append(users, spec.User)
						}

						Expect(paths).To(ContainElement("/etc/cf-assets/healthcheck/healthcheck"))
						Expect(args).To(ContainElement([]string{
							"-port=5432",
							"-timeout=100ms",
							"-uri=/some/path",
							"-readiness-interval=1ms", // 1ms
						}))
						Expect(users).To(ContainElement("root"))
					})

					Context("when the readiness check passes", func() {
						JustBeforeEach(func() {
							readinessCh <- 0
						})

						It("starts the liveness check", func() {
							Eventually(gardenContainer.RunCallCount).Should(Equal(3))
							paths := []string{}
							args := [][]string{}
							for i := 0; i < gardenContainer.RunCallCount(); i++ {
								spec, _ := gardenContainer.RunArgsForCall(i)
								paths = append(paths, spec.Path)
								args = append(args, spec.Args)
							}

							Expect(paths).To(ContainElement("/etc/cf-assets/healthcheck/healthcheck"))
							Expect(args).To(ContainElement([]string{
								"-port=5432",
								"-timeout=100ms",
								"-uri=/some/path",
								"-liveness-interval=1s", // 1ms
							}))
						})

						Context("when the liveness check exits", func() {
							JustBeforeEach(func() {
								livenessCh <- 0
							})

							It("returns an error", func() {
								Eventually(gardenContainer.RunCallCount).Should(Equal(3))
								Eventually(actionProcess.SignalCallCount).ShouldNot(BeZero())
								actionCh <- 0
								Eventually(process.Wait()).Should(Receive(HaveOccurred()))
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
											Port: 5432,
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

							Expect(paths).To(ContainElement("/etc/cf-assets/healthcheck/healthcheck"))
							Expect(args).To(ContainElement([]string{
								"-port=5432",
								"-timeout=1000ms",
								"-readiness-interval=1ms", // 100ms
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

						Expect(paths).To(ContainElement("/etc/cf-assets/healthcheck/healthcheck"))
						Expect(args).To(ContainElement([]string{
							"-port=5432",
							"-timeout=100ms",
							"-readiness-interval=1ms", // 1ms
						}))
					})
				})

				Context("and multiple check definitions exists", func() {
					var (
						otherReadinessProcess *gardenfakes.FakeProcess
						otherReadinessCh      chan int
						otherLivenessProcess  *gardenfakes.FakeProcess
						otherLivenessCh       chan int
					)

					BeforeEach(func() {
						otherReadinessCh = make(chan int)
						otherReadinessProcess = makeProcess(otherReadinessCh)

						otherLivenessCh = make(chan int)
						otherLivenessProcess = makeProcess(otherLivenessCh)

						healthcheckCallCount := int64(0)
						gardenContainer.RunStub = func(spec garden.ProcessSpec, io garden.ProcessIO) (process garden.Process, err error) {
							defer GinkgoRecover()

							switch spec.Path {
							case "/action/path":
								return actionProcess, nil
							case "/etc/cf-assets/healthcheck/healthcheck":
								oldCount := atomic.AddInt64(&healthcheckCallCount, 1)
								switch oldCount {
								case 1:
									return readinessProcess, nil
								case 2:
									return otherReadinessProcess, nil
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
									},
								},
								&models.Check{
									HttpCheck: &models.HTTPCheck{
										Port:             8080,
										RequestTimeoutMs: 100,
									},
								},
							},
						}
					})

					AfterEach(func() {
						close(otherReadinessCh)
						close(otherLivenessCh)
					})

					It("uses the check definition instead of the monitor action", func() {
						Eventually(gardenContainer.RunCallCount).Should(Equal(3))
						paths := []string{}
						args := [][]string{}
						for i := 0; i < gardenContainer.RunCallCount(); i++ {
							spec, _ := gardenContainer.RunArgsForCall(i)
							paths = append(paths, spec.Path)
							args = append(args, spec.Args)
						}

						Expect(paths).To(ContainElement("/etc/cf-assets/healthcheck/healthcheck"))
						Expect(args).To(ContainElement([]string{
							"-port=2222",
							"-timeout=100ms",
							"-readiness-interval=1ms", // 1ms
						}))
						Expect(args).To(ContainElement([]string{
							"-port=8080",
							"-timeout=100ms",
							"-uri=/",
							"-readiness-interval=1ms", // 1ms
						}))
					})

					Context("when one of the readiness checks finish", func() {
						JustBeforeEach(func() {
							Eventually(gardenContainer.RunCallCount).Should(Equal(3))
							readinessCh <- 0
						})

						It("waits for both healthchecks to pass", func() {
							Consistently(gardenContainer.RunCallCount).Should(Equal(3))
						})

						Context("and the other readiness check finish", func() {
							JustBeforeEach(func() {
								otherReadinessCh <- 0
							})

							It("starts the liveness checks", func() {
								Eventually(gardenContainer.RunCallCount).Should(Equal(5))
								paths := []string{}
								args := [][]string{}
								for i := 0; i < gardenContainer.RunCallCount(); i++ {
									spec, _ := gardenContainer.RunArgsForCall(i)
									paths = append(paths, spec.Path)
									args = append(args, spec.Args)
								}

								Expect(paths).To(ContainElement("/etc/cf-assets/healthcheck/healthcheck"))
								Expect(args).To(ContainElement([]string{
									"-port=2222",
									"-timeout=100ms",
									"-liveness-interval=1s", // 1ms
								}))
								Expect(args).To(ContainElement([]string{
									"-port=8080",
									"-timeout=100ms",
									"-uri=/",
									"-liveness-interval=1s", // 1ms
								}))
							})

							Context("when either liveness check exit", func() {
								JustBeforeEach(func() {
									Eventually(gardenContainer.RunCallCount).Should(Equal(5))
									livenessCh <- 0
								})

								It("signals the process and exit", func() {
									Eventually(otherLivenessProcess.SignalCallCount).ShouldNot(BeZero())
									otherLivenessCh <- 0

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
				BeforeEach(func() {
					optimusPrime = transformer.NewTransformer(
						nil, nil, nil, nil, nil, nil,
						os.TempDir(),
						false,
						healthyMonitoringInterval,
						unhealthyMonitoringInterval,
						healthCheckWorkPool,
						clock,
						[]string{"/post-setup/path", "-x", "argument"},
						"jim",
						false,
					)
				})

				It("ignores the check definition and use the MonitorAction", func() {
					clock.WaitForWatcherAndIncrement(unhealthyMonitoringInterval)
					Eventually(gardenContainer.RunCallCount).Should(Equal(2))
					paths := []string{}
					args := [][]string{}
					for i := 0; i < gardenContainer.RunCallCount(); i++ {
						spec, _ := gardenContainer.RunArgsForCall(i)
						paths = append(paths, spec.Path)
						args = append(args, spec.Args)
					}

					Expect(paths).To(ContainElement("/monitor/path"))
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

				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer)
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
		})

		Context("when there is no monitor", func() {
			BeforeEach(func() {
				container.Monitor = nil
			})

			It("does not run the monitor step and immediately says the healthcheck passed", func() {
				gardenContainer.RunReturns(&gardenfakes.FakeProcess{}, nil)

				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer)
				Expect(err).NotTo(HaveOccurred())

				process := ifrit.Background(runner)
				Eventually(process.Ready()).Should(BeClosed())

				Eventually(gardenContainer.RunCallCount).Should(Equal(3))
				processSpec, _ := gardenContainer.RunArgsForCall(2)
				Expect(processSpec.Path).To(Equal("/action/path"))
				Consistently(gardenContainer.RunCallCount).Should(Equal(3))
			})
		})

		Context("MonitorAction", func() {
			var (
				process ifrit.Process
			)

			JustBeforeEach(func() {
				runner, err := optimusPrime.StepsRunner(logger, container, gardenContainer, logStreamer)
				Expect(err).NotTo(HaveOccurred())
				process = ifrit.Background(runner)
			})

			AfterEach(func() {
				ginkgomon.Interrupt(process)
			})

			BeforeEach(func() {
				container.Setup = nil
				container.Monitor = &models.Action{
					ParallelAction: models.Parallel(&models.RunAction{
						Path:              "/monitor/path",
						SuppressLogOutput: true,
					}),
				}
			})

			Context("SuppressLogOutput", func() {
				BeforeEach(func() {
					gardenContainer.RunStub = func(processSpec garden.ProcessSpec, processIO garden.ProcessIO) (garden.Process, error) {
						return &gardenfakes.FakeProcess{}, nil
					}
				})

				JustBeforeEach(func() {
					Eventually(gardenContainer.RunCallCount).Should(Equal(1))
					clock.Increment(1 * time.Second)
					Eventually(gardenContainer.RunCallCount).Should(Equal(2))
				})

				It("is ignored", func() {
					processSpec, processIO := gardenContainer.RunArgsForCall(1)
					Expect(processSpec.Path).To(Equal("/monitor/path"))
					Expect(container.Monitor.RunAction.GetSuppressLogOutput()).Should(BeFalse())
					Expect(processIO.Stdout).ShouldNot(Equal(ioutil.Discard))
					Eventually(process.Ready()).Should(BeClosed())
				})
			})

			Context("logs", func() {
				var (
					exitStatusCh   chan int
					monitorProcess *gardenfakes.FakeProcess
				)

				BeforeEach(func() {
					monitorProcess = &gardenfakes.FakeProcess{}
					actionProcess := &gardenfakes.FakeProcess{}
					exitStatusCh = make(chan int)
					actionProcess.WaitStub = func() (int, error) {
						return <-exitStatusCh, nil
					}

					var monitorProcessIO garden.ProcessIO

					monitorProcess.WaitStub = func() (int, error) {
						if monitorProcess.WaitCallCount() == 2 {
							monitorProcessIO.Stdout.Write([]byte("healthcheck failed\n"))
							return 1, nil
						} else {
							return 0, nil
						}
					}

					gardenContainer.RunStub = func(processSpec garden.ProcessSpec, processIO garden.ProcessIO) (garden.Process, error) {
						if processSpec.Path == "/monitor/path" {
							monitorProcessIO = processIO
							return monitorProcess, nil
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
					clock.Increment(1 * time.Second)
					Eventually(monitorProcess.WaitCallCount).Should(Equal(1))
					clock.Increment(1 * time.Second)
					Eventually(monitorProcess.WaitCallCount).Should(Equal(2))
				})

				It("logs healthcheck error with HEALTH source", func() {
					Eventually(fakeMetronClient.SendAppErrorLogCallCount).Should(Equal(2))
					_, message, sourceName, _ := fakeMetronClient.SendAppErrorLogArgsForCall(0)
					Expect(sourceName).To(Equal("HEALTH"))
					Expect(message).To(Equal("healthcheck failed"))
					_, message, sourceName, _ = fakeMetronClient.SendAppErrorLogArgsForCall(1)
					Expect(sourceName).To(Equal("HEALTH"))
					Expect(message).To(Equal("Exit status 1"))
				})

				It("logs the container lifecycle", func() {
					Eventually(fakeMetronClient.SendAppLogCallCount).Should(Equal(3))
					_, message, _, _ := fakeMetronClient.SendAppLogArgsForCall(0)
					Expect(message).To(Equal("Starting health monitoring of container"))
					_, message, _, _ = fakeMetronClient.SendAppLogArgsForCall(1)
					Expect(message).To(Equal("Container became healthy"))
					_, message, _, _ = fakeMetronClient.SendAppLogArgsForCall(2)
					Expect(message).To(Equal("Container became unhealthy"))
				})
			})
		})
	})
})
