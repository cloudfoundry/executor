package containerstore_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	mfakes "code.cloudfoundry.org/diego-logging-client/testhelpers"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/executor/depot/containerstore/containerstorefakes"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("CredManager", func() {
	var (
		credManager      containerstore.CredManager
		validityPeriod   time.Duration
		CaCert           *x509.Certificate
		privateKey       *rsa.PrivateKey
		reader           io.Reader
		logger           lager.Logger
		clock            *fakeclock.FakeClock
		fakeMetronClient *mfakes.FakeIngressClient
		fakeCredHandler  *containerstorefakes.FakeCredentialHandler
	)

	BeforeEach(func() {

		SetDefaultEventuallyTimeout(10 * time.Second)

		validityPeriod = time.Minute
		fakeMetronClient = &mfakes.FakeIngressClient{}

		fakeCredHandler = &containerstorefakes.FakeCredentialHandler{}

		// we have seen private key generation take a long time in CI, the
		// suspicion is that `getrandom` is getting slower with the increased
		// number of certs we create on the system. This is an experiment to see if
		// using math/rand in the tests will make things less flaky. We are also
		// suspicious that this is affecting cacheddownloader TLS tests
		reader = fastRandReader{}

		logger = lagertest.NewTestLogger("credmanager")
		// Truncate and set to UTC time because of parsing time from certificate
		// and only has second granularity
		clock = fakeclock.NewFakeClock(time.Now().UTC().Truncate(time.Second))

		CaCert, privateKey = createIntermediateCert()
	})

	JustBeforeEach(func() {
		credManager = containerstore.NewCredManager(
			logger,
			fakeMetronClient,
			validityPeriod,
			reader,
			clock,
			CaCert,
			privateKey,
			fakeCredHandler,
		)
	})

	Context("NoopCredManager", func() {
		It("returns a dummy runner", func() {
			container := executor.Container{
				Guid:       fmt.Sprintf("container-guid-%d", GinkgoParallelNode()),
				InternalIP: "127.0.0.1",
				RunInfo: executor.RunInfo{CertificateProperties: executor.CertificateProperties{
					OrganizationalUnit: []string{"app:iamthelizardking"}},
				},
			}

			runner := containerstore.NewNoopCredManager().Runner(logger, container)
			process := ifrit.Background(runner)
			Eventually(process.Ready()).Should(BeClosed())
			Consistently(process.Wait()).ShouldNot(Receive())
			process.Signal(os.Interrupt)
			Eventually(process.Wait()).Should(Receive())
		})
	})

	Context("RemoveCredDir", func() {
		var (
			fakeCredHandler1, fakeCredHandler2 *containerstorefakes.FakeCredentialHandler
		)

		JustBeforeEach(func() {
			fakeCredHandler1 = &containerstorefakes.FakeCredentialHandler{}
			fakeCredHandler2 = &containerstorefakes.FakeCredentialHandler{}

			credManager = containerstore.NewCredManager(
				logger,
				fakeMetronClient,
				validityPeriod,
				reader,
				clock,
				CaCert,
				privateKey,
				fakeCredHandler1,
				fakeCredHandler2,
			)
		})

		It("calls the handlers RemoveDir", func() {
			container := executor.Container{Guid: "guid"}
			credManager.RemoveCredDir(logger, container)
			Expect(fakeCredHandler1.RemoveDirCallCount()).To(Equal(1))
			Expect(fakeCredHandler2.RemoveDirCallCount()).To(Equal(1))
			_, actualContainer := fakeCredHandler1.RemoveDirArgsForCall(0)
			Expect(actualContainer).To(Equal(container))
			_, actualContainer = fakeCredHandler2.RemoveDirArgsForCall(0)
			Expect(actualContainer).To(Equal(container))
		})

		It("if the first handler returned an error continue to call RemoveDir on other handlers", func() {
			fakeCredHandler1.RemoveDirReturns(errors.New("boooom!"))
			credManager.RemoveCredDir(logger, executor.Container{Guid: "guid"})

			Expect(fakeCredHandler1.RemoveDirCallCount()).Should(Equal(1))
			Expect(fakeCredHandler2.RemoveDirCallCount()).Should(Equal(1))
		})

		It("returns an error if one of the handlers returned an error", func() {
			fakeCredHandler2.RemoveDirReturns(errors.New("boooom!"))
			err := credManager.RemoveCredDir(logger, executor.Container{Guid: "guid"})

			Expect(err.Error()).To(Equal("boooom!"))
		})
	})

	Context("CreateCredDir", func() {
		var (
			fakeCredHandler1, fakeCredHandler2 *containerstorefakes.FakeCredentialHandler
		)

		JustBeforeEach(func() {
			fakeCredHandler1 = &containerstorefakes.FakeCredentialHandler{}
			fakeCredHandler2 = &containerstorefakes.FakeCredentialHandler{}

			credManager = containerstore.NewCredManager(
				logger,
				fakeMetronClient,
				validityPeriod,
				reader,
				clock,
				CaCert,
				privateKey,
				fakeCredHandler1,
				fakeCredHandler2,
			)
		})

		It("calls the handlers CreateDir", func() {
			container := executor.Container{Guid: "guid"}
			credManager.CreateCredDir(logger, container)
			Expect(fakeCredHandler1.CreateDirCallCount()).To(Equal(1))
			Expect(fakeCredHandler2.CreateDirCallCount()).To(Equal(1))
			_, actualContainer := fakeCredHandler1.CreateDirArgsForCall(0)
			Expect(actualContainer).To(Equal(container))
			_, actualContainer = fakeCredHandler2.CreateDirArgsForCall(0)
			Expect(actualContainer).To(Equal(container))
		})

		It("collects the bind mounts from all handlers", func() {
			mount1 := garden.BindMount{
				SrcPath: "/src/path1",
				DstPath: "/dst/path1",
			}
			mount2 := garden.BindMount{
				SrcPath: "/src/path2",
				DstPath: "/dst/path2",
			}
			fakeCredHandler1.CreateDirReturns(containerstore.CredentialConfiguration{BindMounts: []garden.BindMount{mount1}}, nil)
			fakeCredHandler2.CreateDirReturns(containerstore.CredentialConfiguration{BindMounts: []garden.BindMount{mount2}}, nil)

			credentialConfiguration, _ := credManager.CreateCredDir(logger, executor.Container{Guid: "guid"})

			Expect(credentialConfiguration.BindMounts).To(ConsistOf(mount1, mount2))
		})

		It("collects tmpfs from all handlers", func() {
			tmpfs1 := garden.Tmpfs{
				Path: "/path/to/tmpfs1",
			}
			tmpfs2 := garden.Tmpfs{
				Path: "/path/to/tmpfs2",
			}
			fakeCredHandler1.CreateDirReturns(containerstore.CredentialConfiguration{Tmpfs: []garden.Tmpfs{tmpfs1}}, nil)
			fakeCredHandler2.CreateDirReturns(containerstore.CredentialConfiguration{Tmpfs: []garden.Tmpfs{tmpfs2}}, nil)

			credentialConfiguration, _ := credManager.CreateCredDir(logger, executor.Container{Guid: "guid"})

			Expect(credentialConfiguration.Tmpfs).To(ConsistOf(tmpfs1, tmpfs2))
		})

		It("collects all environment variables", func() {
			env1 := executor.EnvironmentVariable{Name: "env1", Value: "val1"}
			env2 := executor.EnvironmentVariable{Name: "env2", Value: "val2"}
			fakeCredHandler1.CreateDirReturns(containerstore.CredentialConfiguration{Env: []executor.EnvironmentVariable{env1}}, nil)
			fakeCredHandler2.CreateDirReturns(containerstore.CredentialConfiguration{Env: []executor.EnvironmentVariable{env2}}, nil)

			credentialConfiguration, _ := credManager.CreateCredDir(logger, executor.Container{Guid: "guid"})

			Expect(credentialConfiguration.Env).To(ConsistOf(env1, env2))
		})

		It("returns an error if one of the handlers returned an error", func() {
			fakeCredHandler2.CreateDirReturns(containerstore.CredentialConfiguration{}, errors.New("boooom!"))
			_, err := credManager.CreateCredDir(logger, executor.Container{Guid: "guid"})

			Expect(err).To(MatchError("boooom!"))
		})
	})

	Context("WithCreds", func() {
		var (
			container executor.Container
		)

		BeforeEach(func() {
			container = executor.Container{
				Guid:       fmt.Sprintf("container-guid-%d", GinkgoParallelNode()),
				InternalIP: "127.0.0.1",
				RunInfo: executor.RunInfo{CertificateProperties: executor.CertificateProperties{
					OrganizationalUnit: []string{"app:iamthelizardking"}},
				},
			}
		})

		Context("Runner", func() {
			var (
				containerProcess ifrit.Process
			)

			JustBeforeEach(func() {
				var runner ifrit.Runner
				runner = credManager.Runner(logger, container)
				containerProcess = ifrit.Background(runner)
			})

			AfterEach(func() {
				containerProcess.Signal(os.Interrupt)
				Eventually(containerProcess.Wait()).Should(Receive())
			})

			// TODO: we cannot simulate failing to generate a certificate, but the
			// following should be sufficient
			Context("when generating private key fails", func() {
				BeforeEach(func() {
					reader = io.LimitReader(rand.Reader, 0)
				})

				It("returns an error", func() {
					var err error
					Eventually(containerProcess.Wait()).Should(Receive(&err))
					Expect(err).To(MatchError("EOF"))
				})

				It("emits metrics around failed credential creation", func() {
					var err error
					Eventually(containerProcess.Wait()).Should(Receive(&err))
					Expect(err).To(MatchError("EOF"))

					Expect(fakeMetronClient.IncrementCounterCallCount()).To(Equal(1))
					metric := fakeMetronClient.IncrementCounterArgsForCall(0)
					Expect(metric).To(Equal("CredCreationFailedCount"))
				})
			})

			Context("when the handler returns an error", func() {
				BeforeEach(func() {
					fakeCredHandler.UpdateReturns(errors.New("boooom!"))
				})

				It("the runner exits", func() {
					Eventually(containerProcess.Wait()).Should(Receive(MatchError("boooom!")))
				})
			})

			Context("when runner becomes ready", func() {
				AfterEach(func() {
					containerProcess.Signal(os.Interrupt)
				})

				JustBeforeEach(func() {
					Eventually(containerProcess.Ready()).Should(BeClosed())
				})

				It("emits metrics on successful creation", func() {
					Expect(fakeMetronClient.IncrementCounterCallCount()).To(Equal(1))
					metric := fakeMetronClient.IncrementCounterArgsForCall(0)
					Expect(metric).To(Equal("CredCreationSucceededCount"))

					Expect(fakeMetronClient.SendDurationCallCount()).To(Equal(1))
					metric, value, _ := fakeMetronClient.SendDurationArgsForCall(0)
					Expect(metric).To(Equal("CredCreationSucceededDuration"))
					Expect(value).To(BeNumerically(">=", 0))
				})

				It("calls the handler with the initiali credentials", func() {
					Eventually(fakeCredHandler.UpdateCallCount).Should(Equal(1))
				})

				Context("when the certificate is about to expire", func() {
					var (
						credBefore containerstore.Credential
					)

					JustBeforeEach(func() {
						Eventually(fakeCredHandler.UpdateCallCount).Should(Equal(1))

						Eventually(containerProcess.Ready()).Should(BeClosed())
						credBefore, _ = fakeCredHandler.UpdateArgsForCall(0)
					})

					testCredentialRotation := func(dur time.Duration) {
						callCount := fakeCredHandler.UpdateCallCount()

						var actualContainer executor.Container
						credBefore, actualContainer = fakeCredHandler.UpdateArgsForCall(callCount - 1)

						Expect(actualContainer).To(Equal(container))

						certBefore, _ := parseCert(credBefore)
						increment := certBefore.NotAfter.Add(-dur).Sub(clock.Now())
						Expect(increment).To(BeNumerically(">", 0))
						clock.WaitForWatcherAndIncrement(increment)

						Eventually(fakeCredHandler.UpdateCallCount).Should(Equal(callCount + 1))

						cred, actualContainer := fakeCredHandler.UpdateArgsForCall(callCount)
						Expect(actualContainer).To(Equal(container))

						Expect(cred.Cert).NotTo(Equal(credBefore.Cert))
						Expect(cred.Key).NotTo(Equal(credBefore.Key))

						cert, _ := parseCert(cred)
						Expect(cert.SerialNumber).NotTo(Equal(certBefore.SerialNumber))
					}

					testNoCredentialRotation := func(dur time.Duration) {
						callCount := fakeCredHandler.UpdateCallCount()
						credBefore, _ = fakeCredHandler.UpdateArgsForCall(callCount - 1)

						cert, _ := parseCert(credBefore)
						increment := cert.NotAfter.Add(-dur).Sub(clock.Now())
						Expect(increment).To(BeNumerically(">", 0))
						clock.WaitForWatcherAndIncrement(increment)

						Consistently(fakeCredHandler.UpdateCallCount).Should(Equal(callCount))
					}

					Context("when the handler returns an error", func() {
						JustBeforeEach(func() {
							fakeCredHandler.UpdateReturnsOnCall(1, errors.New("boooom!"))
							certBefore, _ := parseCert(credBefore)
							increment := certBefore.NotAfter.Sub(clock.Now())
							clock.WaitForWatcherAndIncrement(increment)
						})

						It("the runner exits", func() {
							Eventually(containerProcess.Wait()).Should(Receive(MatchError("boooom!")))
						})
					})

					Context("when the certificate validity is less than 4 hours", func() {
						BeforeEach(func() {
							validityPeriod = time.Minute
						})

						Context("when 15 seconds prior to expiry", func() {
							It("does not rotate the credentials", func() {
								testNoCredentialRotation(15 * time.Second)
							})
						})

						Context("when 5 seconds prior to expiry", func() {
							It("rotates the certificates", func() {
								testCredentialRotation(5 * time.Second)
							})

							It("emits metrics on successful creation", func() {
								cert, _ := parseCert(credBefore)
								increment := cert.NotAfter.Add(-5 * time.Second).Sub(clock.Now())
								Expect(increment).To(BeNumerically(">", 0))
								clock.WaitForWatcherAndIncrement(increment)

								Eventually(fakeMetronClient.IncrementCounterCallCount).Should(Equal(2))
								metric := fakeMetronClient.IncrementCounterArgsForCall(1)
								Expect(metric).To(Equal("CredCreationSucceededCount"))

								Expect(fakeMetronClient.SendDurationCallCount()).To(Equal(2))
								metric, value, _ := fakeMetronClient.SendDurationArgsForCall(1)
								Expect(metric).To(Equal("CredCreationSucceededDuration"))
								Expect(value).To(BeNumerically(">=", 0))
							})
						})

						// test timer reset logic
						Context("after credential rotation", func() {
							JustBeforeEach(func() {
								testCredentialRotation(5 * time.Second)
							})

							Context("when 5 seconds prior to expiry", func() {
								It("rotates the certs", func() {
									testCredentialRotation(5 * time.Second)
								})
							})

							Context("when 15 seconds prior to expiry", func() {
								It("does not rotate the credentials", func() {
									testNoCredentialRotation(15 * time.Second)
								})
							})
						})
					})

					Context("when certificate validity is longer than 4 hours", func() {
						BeforeEach(func() {
							validityPeriod = 24 * time.Hour
						})

						Context("when 90 minutes prior to expiry", func() {
							It("does not rotate the credentials", func() {
								testNoCredentialRotation(90 * time.Minute)
							})
						})

						Context("when 30 minutes prior to expiry", func() {
							It("rotates the certs", func() {
								testCredentialRotation(30 * time.Minute)
							})
						})
					})
				})

				Describe("the certificate", func() {
					var (
						cert *x509.Certificate
						rest []byte
					)

					JustBeforeEach(func() {
						Eventually(containerProcess.Ready()).Should(BeClosed())

						Eventually(fakeCredHandler.UpdateCallCount).Should(Equal(1))
						cred, actualContainer := fakeCredHandler.UpdateArgsForCall(0)
						Expect(actualContainer).To(Equal(container))
						cert, rest = parseCert(cred)
					})

					It("has the container ip", func() {
						ip := net.ParseIP(container.InternalIP)
						Expect(cert.IPAddresses).To(ContainElement(ip.To4()))
					})

					It("has all required usages in the KU & EKU fields", func() {
						Expect(cert.ExtKeyUsage).To(ContainElement(x509.ExtKeyUsageClientAuth))
						Expect(cert.ExtKeyUsage).To(ContainElement(x509.ExtKeyUsageServerAuth))
						Expect(cert.KeyUsage).To(Equal(x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageKeyAgreement))
					})

					It("signed by the rep intermediate CA", func() {
						CaCertPool := x509.NewCertPool()
						CaCertPool.AddCert(CaCert)
						verifyOpts := x509.VerifyOptions{Roots: CaCertPool}
						Expect(cert.CheckSignatureFrom(CaCert)).To(Succeed())
						_, err := cert.Verify(verifyOpts)
						Expect(err).NotTo(HaveOccurred())
					})

					It("common name should be set to the container guid", func() {
						Expect(cert.Subject.CommonName).To(Equal(container.Guid))
					})

					It("DNS SAN should be set to the container guid", func() {
						Expect(cert.DNSNames).To(ContainElement(container.Guid))
					})

					It("expires in after the configured validity period", func() {
						Expect(cert.NotAfter).To(Equal(clock.Now().Add(validityPeriod)))
					})

					It("not before is set to current timestamp", func() {
						Expect(cert.NotBefore).To(Equal(clock.Now()))
					})

					It("has the rep intermediate CA", func() {
						block, rest := pem.Decode(rest)
						Expect(block).NotTo(BeNil())
						Expect(rest).To(BeEmpty())
						Expect(block.Type).To(Equal("CERTIFICATE"))
						certs, err := x509.ParseCertificates(block.Bytes)
						Expect(err).NotTo(HaveOccurred())
						Expect(certs).To(HaveLen(1))
						cert = certs[0]
						Expect(cert).To(Equal(CaCert))
					})

					It("has the app guid in the subject's organizational units", func() {
						Expect(cert.Subject.OrganizationalUnit).To(ContainElement("app:iamthelizardking"))
					})

					Context("when the container doesn't have an internal ip", func() {
						Context("when the container has an external IP", func() {
							BeforeEach(func() {
								container = executor.Container{
									Guid:       fmt.Sprintf("container-guid-%d", GinkgoParallelNode()),
									InternalIP: "",
									ExternalIP: "54.23.123.234",
									RunInfo: executor.RunInfo{CertificateProperties: executor.CertificateProperties{
										OrganizationalUnit: []string{"app:iamthelizardking"}},
									},
								}
							})

							It("has the external ip", func() {
								ip := net.ParseIP(container.ExternalIP)
								Expect(cert.IPAddresses).To(ContainElement(ip.To4()))
							})

							It("does not have the empty internal ip", func() {
								ip := net.ParseIP(container.InternalIP)
								Expect(cert.IPAddresses).NotTo(ContainElement(ip.To4()))
							})
						})
						Context("when the container doesn't have an external ip", func() {
							BeforeEach(func() {
								container = executor.Container{
									Guid:       fmt.Sprintf("container-guid-%d", GinkgoParallelNode()),
									InternalIP: "",
									ExternalIP: "",
									RunInfo: executor.RunInfo{CertificateProperties: executor.CertificateProperties{
										OrganizationalUnit: []string{"app:iamthelizardking"}},
									},
								}
							})

							It("has no SubjectAltName", func() {
								Expect(cert.IPAddresses).To(BeEmpty())
							})

						})
					})
				})
			})

			Context("when signalled", func() {
				JustBeforeEach(func() {
					Eventually(containerProcess.Ready()).Should(BeClosed())
					containerProcess.Signal(os.Interrupt)
				})

				// deleting the directory this early can cause failures on windows 1803, see #156406881
				It("does not call RemoveDir on the handlers", func() {
					Eventually(fakeCredHandler.RemoveDirCallCount).Should(BeZero())
				})

				It("Generates an invalid cert and sends the invalid cert on the cred channel", func() {
					Eventually(fakeCredHandler.CloseCallCount).Should(Equal(1))

					cred, actualContainer := fakeCredHandler.CloseArgsForCall(0)
					Expect(actualContainer).To(Equal(container))

					block, _ := pem.Decode([]byte(cred.Cert))
					Expect(block).NotTo(BeNil())
					certs, err := x509.ParseCertificates(block.Bytes)
					Expect(err).NotTo(HaveOccurred())
					cert := certs[0]

					Expect(cert.Subject.CommonName).To(BeEmpty())
				})
			})
		})
	})
})

func createIntermediateCert() (*x509.Certificate, *rsa.PrivateKey) {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	Expect(err).NotTo(HaveOccurred())

	template := &x509.Certificate{
		IsCA: true,
		BasicConstraintsValid: true,
		SerialNumber:          big.NewInt(1),
		NotAfter:              time.Now().Add(36 * time.Hour),
	}
	certBytes, err := x509.CreateCertificate(rand.Reader, template, template, privateKey.Public(), privateKey)
	Expect(err).NotTo(HaveOccurred())

	certs, err := x509.ParseCertificates(certBytes)
	Expect(err).NotTo(HaveOccurred())
	Expect(certs).To(HaveLen(1))
	return certs[0], privateKey
}

func parseCert(cred containerstore.Credential) (*x509.Certificate, []byte) {
	var block *pem.Block
	var rest []byte
	block, rest = pem.Decode([]byte(cred.Cert))
	Expect(block).NotTo(BeNil())
	Expect(block.Type).To(Equal("CERTIFICATE"))
	certs, err := x509.ParseCertificates(block.Bytes)
	Expect(err).NotTo(HaveOccurred())
	return certs[0], rest
}
