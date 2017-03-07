package containerstore_test

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"code.cloudfoundry.org/clock/fakeclock"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
	"code.cloudfoundry.org/lager/lagertest"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("CredManager", func() {
	var (
		credManager    containerstore.CredManager
		validityPeriod time.Duration
		CaCert         *x509.Certificate
		privateKey     *rsa.PrivateKey
		tmpdir         string
		logger         lager.Logger
		clock          *fakeclock.FakeClock
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = ioutil.TempDir("", "credsmanager")
		Expect(err).ToNot(HaveOccurred())

		validityPeriod = time.Minute

		logger = lagertest.NewTestLogger("credmanager")
		// Truncate and set to UTC time because of parsing time from certificate
		// and only has second granularity
		clock = fakeclock.NewFakeClock(time.Now().UTC().Truncate(time.Second))

		CaCert, privateKey = createIntermediateCert()
		credManager = containerstore.NewCredManager(tmpdir, validityPeriod, rand.Reader, clock, CaCert, privateKey, "containerpath")
	})

	AfterEach(func() {
		os.RemoveAll(tmpdir)
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
				if runtime.GOOS == "windows" {
					Skip("Chmod does not work on windows")
				}

				os.Chmod(tmpdir, 0400)
			})

			It("returns an error", func() {
				_, _, err := credManager.CreateCredDir(logger, executor.Container{Guid: "somefailure"})
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Context("GenerateCreds", func() {
		var container executor.Container

		BeforeEach(func() {
			container = executor.Container{
				Guid:       "container-guid",
				InternalIP: "127.0.0.1",
				RunInfo: executor.RunInfo{CertificateProperties: executor.CertificateProperties{
					OrganizationalUnit: []string{"app:iamthelizardking"}},
				},
			}
			_, _, err := credManager.CreateCredDir(logger, container)
			Expect(err).NotTo(HaveOccurred())
		})

		It("puts private key into container directory", func() {
			err := credManager.GenerateCreds(logger, container)
			Expect(err).NotTo(HaveOccurred())

			certPath := filepath.Join(tmpdir, container.Guid)

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

		Context("when generating private key fails", func() {
			BeforeEach(func() {
				reader := io.LimitReader(rand.Reader, 0)
				credManager = containerstore.NewCredManager(tmpdir, validityPeriod, reader, clock, CaCert, privateKey, "")
			})

			It("returns an error", func() {
				err := credManager.GenerateCreds(logger, container)
				Expect(err).To(MatchError("EOF"))
			})
		})

		It("signs and puts the certificate into container directory", func() {
			err := credManager.GenerateCreds(logger, container)
			Expect(err).NotTo(HaveOccurred())

			certPath := filepath.Join(tmpdir, container.Guid)

			certFile := filepath.Join(certPath, "instance.crt")
			Expect(certFile).To(BeARegularFile())
		})

		Describe("the cert", func() {
			var (
				cert *x509.Certificate
				rest []byte
			)

			BeforeEach(func() {
				err := credManager.GenerateCreds(logger, container)
				Expect(err).NotTo(HaveOccurred())
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

			It("expires in after the configured validity period", func() {
				Expect(cert.NotAfter).To(Equal(clock.Now().Add(validityPeriod)))
			})

			It("not before is set to current timestamp", func() {
				Expect(cert.NotBefore).To(Equal(clock.Now()))
			})

			It("sets the serial number to the container guid", func() {
				expected := big.NewInt(0)
				expected.SetBytes([]byte(container.Guid))

				Expect(expected).To(Equal(cert.SerialNumber))
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
		})
	})

	Context("RemoveCreds", func() {
		var container executor.Container

		BeforeEach(func() {
			container = executor.Container{
				Guid:       "container-guid",
				InternalIP: "127.0.0.1",
			}
		})

		It("removes container credentials from the filesystem", func() {
			certMount, _, err := credManager.CreateCredDir(logger, container)
			Expect(err).NotTo(HaveOccurred())
			Expect(certMount[0].SrcPath).To(BeADirectory())

			credManager.RemoveCreds(logger, container)
			Expect(certMount[0].SrcPath).ToNot(BeADirectory())
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
