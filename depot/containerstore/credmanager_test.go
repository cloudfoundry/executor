package containerstore_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	mfakes "code.cloudfoundry.org/diego-logging-client/testhelpers"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/tedsuo/ifrit"
)

var _ = Describe("CredManager", func() {
	var (
		credManager        containerstore.CredManager
		validityPeriod     time.Duration
		CaCert             *x509.Certificate
		privateKey         *rsa.PrivateKey
		reader             io.Reader
		tmpdir             string
		containerMountPath string
		logger             lager.Logger
		clock              *fakeclock.FakeClock
		fakeMetronClient   *mfakes.FakeIngressClient
	)

	BeforeEach(func() {
		var err error

		SetDefaultEventuallyTimeout(10 * time.Second)

		tmpdir, err = ioutil.TempDir("", "credsmanager")
		Expect(err).ToNot(HaveOccurred())

		validityPeriod = time.Minute
		containerMountPath = "containerpath"
		fakeMetronClient = &mfakes.FakeIngressClient{}

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
			tmpdir,
			validityPeriod,
			reader,
			clock,
			CaCert,
			privateKey,
			containerMountPath,
		)
	})

	AfterEach(func() {
		os.RemoveAll(tmpdir)
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

			runner, _ := containerstore.NewNoopCredManager().Runner(logger, container)
			process := ifrit.Background(runner)
			Eventually(process.Ready()).Should(BeClosed())
			Consistently(process.Wait()).ShouldNot(Receive())
			process.Signal(os.Interrupt)
			Eventually(process.Wait()).Should(Receive())
		})
	})

	Context("CreateCredDir", func() {
		It("returns a valid directory path", func() {
			mount, _, err := credManager.CreateCredDir(logger, executor.Container{Guid: "guid"})
			Expect(err).To(Succeed())

			Expect(mount).To(HaveLen(1))
			Expect(mount[0].SrcPath).To(BeADirectory())
			Expect(mount[0].DstPath).To(Equal("containerpath"))
			Expect(mount[0].Mode).To(Equal(garden.BindMountModeRO))
			Expect(mount[0].Origin).To(Equal(garden.BindMountOriginHost))
		})

		It("returns CF_INSTANCE_CERT and CF_INSTANCE_KEY environment variable values", func() {
			_, envVariables, err := credManager.CreateCredDir(logger, executor.Container{Guid: "guid"})
			Expect(err).To(Succeed())

			Expect(envVariables).To(HaveLen(2))
			values := map[string]string{}
			values[envVariables[0].Name] = envVariables[0].Value
			values[envVariables[1].Name] = envVariables[1].Value
			Expect(values).To(Equal(map[string]string{
				"CF_INSTANCE_CERT": "containerpath/instance.crt",
				"CF_INSTANCE_KEY":  "containerpath/instance.key",
			}))
		})

		Context("when making directory fails", func() {
			BeforeEach(func() {
				tmpdir = filepath.Join(tmpdir, "creds")
			})

			It("returns an error", func() {
				_, _, err := credManager.CreateCredDir(logger, executor.Container{Guid: "somefailure"})
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Context("WithCreds", func() {
		var (
			container        executor.Container
			certMount        []garden.BindMount
			err              error
			certPath         string
			rotatingCredChan <-chan containerstore.Credential
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

		JustBeforeEach(func() {
			certMount, _, err = credManager.CreateCredDir(logger, container)
			Expect(err).NotTo(HaveOccurred())
			Expect(certMount[0].SrcPath).To(BeADirectory())

			certPath = filepath.Join(tmpdir, container.Guid)
		})

		Context("Runner", func() {
			var (
				containerProcess ifrit.Process
			)

			JustBeforeEach(func() {
				var runner ifrit.Runner
				runner, rotatingCredChan = credManager.Runner(logger, container)
				containerProcess = ifrit.Background(runner)
			})

			AfterEach(func() {
				containerProcess.Signal(os.Interrupt)
				Eventually(containerProcess.Wait()).Should(Receive())
			})

			It("puts private key into container directory", func() {
				Eventually(containerProcess.Ready()).Should(BeClosed())

				keyFile := filepath.Join(certPath, "instance.key")
				data, err := ioutil.ReadFile(keyFile)
				Expect(err).NotTo(HaveOccurred())

				block, rest := pem.Decode(data)
				Expect(block).NotTo(BeNil())
				Expect(rest).To(BeEmpty())

				Expect(block.Type).To(Equal("RSA PRIVATE KEY"))
				key, err := x509.ParsePKCS1PrivateKey(block.Bytes)
				Expect(err).NotTo(HaveOccurred())

				var bits int
				for _, p := range key.Primes {
					bits += p.BitLen()
				}
				Expect(bits).To(Equal(2048))
			})

			It("signs and puts the certificate into container directory", func() {
				Eventually(containerProcess.Ready()).Should(BeClosed())

				certFile := filepath.Join(certPath, "instance.crt")
				Expect(certFile).To(BeARegularFile())
			})

			It("emits metrics on successful creation", func() {
				Eventually(containerProcess.Ready()).Should(BeClosed())

				Expect(fakeMetronClient.IncrementCounterCallCount()).To(Equal(1))
				metric := fakeMetronClient.IncrementCounterArgsForCall(0)
				Expect(metric).To(Equal("CredCreationSucceededCount"))

				Expect(fakeMetronClient.SendDurationCallCount()).To(Equal(1))
				metric, value, _ := fakeMetronClient.SendDurationArgsForCall(0)
				Expect(metric).To(Equal("CredCreationSucceededDuration"))
				Expect(value).To(BeNumerically(">=", 0))
			})

			Context("when the certificate is about to expire", func() {
				var (
					keyBefore             []byte
					certBefore            []byte
					serialNumber          *big.Int
					durationPriorToExpiry time.Duration
				)

				readKeyAndCert := func() ([]byte, []byte) {
					keyFile := filepath.Join(certPath, "instance.key")
					key, err := ioutil.ReadFile(keyFile)
					ExpectWithOffset(1, err).NotTo(HaveOccurred())
					certFile := filepath.Join(certPath, "instance.crt")
					cert, err := ioutil.ReadFile(certFile)
					ExpectWithOffset(1, err).NotTo(HaveOccurred())
					return key, cert
				}

				JustBeforeEach(func() {
					Eventually(containerProcess.Ready()).Should(BeClosed())
					keyBefore, certBefore = readKeyAndCert()

					var cred containerstore.Credential
					Eventually(rotatingCredChan).Should(Receive(&cred))
					Expect(cred.Key).To(Equal(string(keyBefore)))
					Expect(cred.Cert).To(Equal(string(certBefore)))

					var block *pem.Block
					block, _ = pem.Decode(certBefore)
					certs, err := x509.ParseCertificates(block.Bytes)
					Expect(err).NotTo(HaveOccurred())
					cert := certs[0]
					increment := cert.NotAfter.Add(-durationPriorToExpiry).Sub(clock.Now())

					Expect(increment).To(BeNumerically(">", 0))
					clock.WaitForWatcherAndIncrement(increment)
				})

				testCredentialRotation := func() {
					It("generates a new certificate and keypair", func() {
						var cred containerstore.Credential
						Eventually(rotatingCredChan).Should(Receive(&cred))

						var key, cert []byte

						Eventually(func() []byte {
							key, _ = readKeyAndCert()
							return key
						}).ShouldNot(Equal(keyBefore))

						Eventually(func() []byte {
							_, cert = readKeyAndCert()
							return cert
						}).ShouldNot(Equal(certBefore))

						Expect(cred.Key).To(Equal(string(key)))
						Expect(cred.Cert).To(Equal(string(cert)))

						var block *pem.Block
						_, certAfter := readKeyAndCert()
						block, _ = pem.Decode(certAfter)
						certs, err := x509.ParseCertificates(block.Bytes)
						Expect(err).NotTo(HaveOccurred())
						x509Cert := certs[0]
						Expect(x509Cert.SerialNumber).ToNot(Equal(serialNumber))
					})
				}

				testNoCredentialRotation := func() {
					It("does not rotate the credentials", func() {
						Consistently(func() []byte {
							keyFile := filepath.Join(certPath, "instance.key")
							keyAfter, err := ioutil.ReadFile(keyFile)
							Expect(err).NotTo(HaveOccurred())
							return keyAfter
						}).Should(Equal(keyBefore))

						Consistently(func() []byte {
							certFile := filepath.Join(certPath, "instance.crt")
							certAfter, err := ioutil.ReadFile(certFile)
							Expect(err).NotTo(HaveOccurred())
							return certAfter
						}).Should(Equal(certBefore))
					})
				}

				Context("when the certificate validity is less than 4 hours", func() {
					BeforeEach(func() {
						validityPeriod = time.Minute
					})

					Context("when 15 seconds prior to expiry", func() {
						BeforeEach(func() {
							durationPriorToExpiry = 15 * time.Second
						})

						testNoCredentialRotation()
					})

					Context("when 5 seconds prior to expiry", func() {
						BeforeEach(func() {
							durationPriorToExpiry = 5 * time.Second
						})

						testCredentialRotation()

						It("emits metrics on successful creation", func() {
							Eventually(rotatingCredChan).Should(Receive())
							Expect(fakeMetronClient.IncrementCounterCallCount()).To(Equal(2))
							metric := fakeMetronClient.IncrementCounterArgsForCall(1)
							Expect(metric).To(Equal("CredCreationSucceededCount"))

							Expect(fakeMetronClient.SendDurationCallCount()).To(Equal(2))
							metric, value, _ := fakeMetronClient.SendDurationArgsForCall(1)
							Expect(metric).To(Equal("CredCreationSucceededDuration"))
							Expect(value).To(BeNumerically(">=", 0))
						})

						It("rotates the cert atomically", func() {
							before := string(certBefore)
							var after string
							// similar to eventually but faster, to ensure we sample the cert
							// file as soon as it is overwritten. stop as soon as we see a
							// change to the file
							//
							// arbitrary limit to prevent infinite loop
							limit := 100000
							for ; limit > 0; limit-- {
								_, certBytes := readKeyAndCert()
								after = string(certBytes)
								if after != before {
									break
								}
							}
							Expect(limit).To(BeNumerically(">", 0))

							block, _ := pem.Decode([]byte(after))
							Expect(block).NotTo(BeNil(), "invalid data in cert file")
							_, err := x509.ParseCertificates(block.Bytes)
							Expect(err).NotTo(HaveOccurred())
						})

						It("rotates the key atomically", func() {
							before := string(keyBefore)
							var after string
							// similar to eventually but faster, to ensure we sample the key
							// file as soon as it is overwritten. stop as soon as we see a
							// change to the file
							//
							// arbitrary limit to prevent infinite loop
							limit := 100000
							for ; limit > 0; limit-- {
								keyBytes, _ := readKeyAndCert()
								after = string(keyBytes)
								if after != before {
									break
								}
							}
							Expect(limit).To(BeNumerically(">", 0))

							block, _ := pem.Decode([]byte(after))
							Expect(block).NotTo(BeNil(), "invalid data in key file")
							_, err := x509.ParsePKCS1PrivateKey(block.Bytes)
							Expect(err).NotTo(HaveOccurred())
						})

						// test timer reset logic
						Context("when 5 seconds prior to expiry", func() {
							// wait for the cert to rotate from the outer context before running this test
							JustBeforeEach(func() {
								keyBefore, certBefore = readKeyAndCert()
								clock.WaitForWatcherAndIncrement(5 * time.Second)
							})

							testCredentialRotation()
						})

						Context("when 15 seconds prior to expiry", func() {
							JustBeforeEach(func() {
								// wait for the cert to rotate from the outer context before running this test
								Eventually(rotatingCredChan).Should(Receive())
								keyBefore, certBefore = readKeyAndCert()
								clock.WaitForWatcherAndIncrement(15 * time.Second)
							})

							testNoCredentialRotation()
						})
					})
				})

				Context("when certificate validity is longer than 4 hours", func() {
					BeforeEach(func() {
						validityPeriod = 24 * time.Hour
					})

					Context("when 90 minutes prior to expiry", func() {
						BeforeEach(func() {
							durationPriorToExpiry = 90 * time.Minute
						})

						testNoCredentialRotation()
					})

					Context("when 30 minutes prior to expiry", func() {
						BeforeEach(func() {
							durationPriorToExpiry = 30 * time.Minute
						})

						testCredentialRotation()
					})
				})
			})

			Context("when regenerating certificate and key fails", func() {
				JustBeforeEach(func() {
					Eventually(containerProcess.Ready()).Should(BeClosed())
					Eventually(filepath.Join(certPath, "instance.key")).Should(BeARegularFile())
					Expect(os.RemoveAll(tmpdir)).To(Succeed())
					clock.WaitForWatcherAndIncrement(1 * time.Hour)
				})

				It("returns an error", func() {
					var err error
					Eventually(containerProcess.Wait()).Should(Receive(&err))
					Expect(err).To(HaveOccurred())
				})

				It("emits metrics around failed credential creation", func() {
					Eventually(containerProcess.Wait()).Should(Receive())
					Expect(fakeMetronClient.IncrementCounterCallCount()).To(Equal(2))
					metric := fakeMetronClient.IncrementCounterArgsForCall(1)
					Expect(metric).To(Equal("CredCreationFailedCount"))
				})
			})

			Context("when signalled", func() {
				JustBeforeEach(func() {
					Eventually(containerProcess.Ready()).Should(BeClosed())
					Eventually(certMount[0].SrcPath).Should(BeADirectory())
					containerProcess.Signal(os.Interrupt)
				})

				It("removes container credentials from the filesystem", func() {
					Eventually(certMount[0].SrcPath).ShouldNot(BeADirectory())
				})

				It("closes the rotating cred channel", func() {
					Eventually(rotatingCredChan).Should(BeClosed())
				})
			})

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

			Describe("the certificate", func() {
				var (
					cert *x509.Certificate
					rest []byte
				)

				JustBeforeEach(func() {
					Eventually(containerProcess.Ready()).Should(BeClosed())

					certFile := filepath.Join(tmpdir, container.Guid, "instance.crt")
					data, err := ioutil.ReadFile(certFile)
					Expect(err).NotTo(HaveOccurred())
					var block *pem.Block
					block, rest = pem.Decode(data)
					Expect(err).NotTo(HaveOccurred())
					Expect(block).NotTo(BeNil())
					Expect(block.Type).To(Equal("CERTIFICATE"))
					certs, err := x509.ParseCertificates(block.Bytes)
					Expect(err).NotTo(HaveOccurred())
					Expect(certs).To(HaveLen(1))
					cert = certs[0]
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
