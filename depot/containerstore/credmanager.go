package containerstore

import (
	"bytes"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"io/ioutil"
	"math/big"
	"net"
	"os"
	"path"
	"path/filepath"
	"time"

	uuid "github.com/nu7hatch/gouuid"
	"github.com/tedsuo/ifrit"

	"code.cloudfoundry.org/clock"
	loggingclient "code.cloudfoundry.org/diego-logging-client"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
)

const (
	CredCreationSucceededCount    = "CredCreationSucceededCount"
	CredCreationSucceededDuration = "CredCreationSucceededDuration"
	CredCreationFailedCount       = "CredCreationFailedCount"
)

type Credential struct {
	Cert string
	Key  string
}

//go:generate counterfeiter -o containerstorefakes/fake_cred_manager.go . CredManager
type CredManager interface {
	CreateCredDir(lager.Logger, executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error)
	RemoveCredDir(lager.Logger, executor.Container) error
	Runner(lager.Logger, executor.Container) (ifrit.Runner, <-chan Credential)
}

type noopManager struct{}

func NewNoopCredManager() CredManager {
	return &noopManager{}
}

func (c *noopManager) CreateCredDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error) {
	return nil, nil, nil
}

func (c *noopManager) RemoveCredDir(logger lager.Logger, container executor.Container) error {
	return nil
}

func (c *noopManager) Runner(lager.Logger, executor.Container) (ifrit.Runner, <-chan Credential) {
	return ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		close(ready)
		<-signals
		return nil
	}), nil
}

type credManager struct {
	logger             lager.Logger
	metronClient       loggingclient.IngressClient
	credDir            string
	validityPeriod     time.Duration
	entropyReader      io.Reader
	clock              clock.Clock
	CaCert             *x509.Certificate
	privateKey         *rsa.PrivateKey
	containerMountPath string
}

func NewCredManager(
	logger lager.Logger,
	metronClient loggingclient.IngressClient,
	credDir string,
	validityPeriod time.Duration,
	entropyReader io.Reader,
	clock clock.Clock,
	CaCert *x509.Certificate,
	privateKey *rsa.PrivateKey,
	containerMountPath string,
) CredManager {
	return &credManager{
		logger:             logger,
		metronClient:       metronClient,
		credDir:            credDir,
		validityPeriod:     validityPeriod,
		entropyReader:      entropyReader,
		clock:              clock,
		CaCert:             CaCert,
		privateKey:         privateKey,
		containerMountPath: containerMountPath,
	}
}

func calculateCredentialRotationPeriod(validityPeriod time.Duration) time.Duration {
	if validityPeriod > 4*time.Hour {
		return validityPeriod - 30*time.Minute
	} else {
		eighth := validityPeriod / 8
		return validityPeriod - eighth
	}
}

func (c *credManager) Runner(logger lager.Logger, container executor.Container) (ifrit.Runner, <-chan Credential) {
	rotatingCred := make(chan Credential, 1)
	runner := ifrit.RunFunc(func(signals <-chan os.Signal, ready chan<- struct{}) error {
		logger = logger.Session("cred-manager-runner")
		logger.Info("starting")
		defer logger.Info("finished")

		start := c.clock.Now()
		creds, err := c.generateAndRotateCredsOnDisk(logger, container)
		duration := c.clock.Since(start)
		if err != nil {
			logger.Error("failed-to-generate-credentials", err)
			c.metronClient.IncrementCounter(CredCreationFailedCount)
			return err
		}

		c.metronClient.IncrementCounter(CredCreationSucceededCount)
		c.metronClient.SendDuration(CredCreationSucceededDuration, duration)

		rotationDuration := calculateCredentialRotationPeriod(c.validityPeriod)
		regenCertTimer := c.clock.NewTimer(rotationDuration)

		close(ready)
		logger.Info("started")
		rotatingCred <- creds

		regenLogger := logger.Session("regenerating-cert-and-key")
		for {
			select {
			case <-regenCertTimer.C():
				regenLogger.Debug("started")
				start := c.clock.Now()
				creds, err := c.generateAndRotateCredsOnDisk(regenLogger, container)
				duration := c.clock.Since(start)
				if err != nil {
					regenLogger.Error("failed-to-generate-credentials", err)
					c.metronClient.IncrementCounter(CredCreationFailedCount)
					return err
				}
				c.metronClient.IncrementCounter(CredCreationSucceededCount)
				c.metronClient.SendDuration(CredCreationSucceededDuration, duration)

				rotationDuration = calculateCredentialRotationPeriod(c.validityPeriod)
				regenCertTimer.Reset(rotationDuration)
				rotatingCred <- creds
				regenLogger.Debug("completed")
			case signal := <-signals:
				invalidTime := c.clock.Now().Add(-time.Hour)
				creds, err := c.generateCreds(logger, container, ioutil.Discard, ioutil.Discard, invalidTime, invalidTime)
				if err != nil {
					regenLogger.Error("failed-to-generate-credentials", err)
					c.metronClient.IncrementCounter(CredCreationFailedCount)
					return err
				}
				rotatingCred <- creds
				logger.Info("signalled", lager.Data{"signal": signal.String()})
				close(rotatingCred)
				return nil
			}
		}
	})

	return runner, rotatingCred
}

func (c *credManager) CreateCredDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error) {
	logger = logger.Session("create-cred-dir")
	logger.Info("starting")
	defer logger.Info("complete")

	containerDir := filepath.Join(c.credDir, container.Guid)
	err := os.Mkdir(containerDir, 0755)
	if err != nil {
		return nil, nil, err
	}

	return []garden.BindMount{
			{
				SrcPath: containerDir,
				DstPath: c.containerMountPath,
				Mode:    garden.BindMountModeRO,
				Origin:  garden.BindMountOriginHost,
			},
		}, []executor.EnvironmentVariable{
			{Name: "CF_INSTANCE_CERT", Value: path.Join(c.containerMountPath, "instance.crt")},
			{Name: "CF_INSTANCE_KEY", Value: path.Join(c.containerMountPath, "instance.key")},
		}, nil
}

const (
	certificatePEMBlockType = "CERTIFICATE"
	privateKeyPEMBlockType  = "RSA PRIVATE KEY"
)

func (c *credManager) generateAndRotateCredsOnDisk(logger lager.Logger, container executor.Container) (Credential, error) {
	instanceKeyPath := filepath.Join(c.credDir, container.Guid, "instance.key")
	tmpInstanceKeyPath := instanceKeyPath + ".tmp"
	certificatePath := filepath.Join(c.credDir, container.Guid, "instance.crt")
	tmpCertificatePath := certificatePath + ".tmp"

	instanceKey, err := os.Create(tmpInstanceKeyPath)
	if err != nil {
		return Credential{}, err
	}
	defer instanceKey.Close()

	certificate, err := os.Create(tmpCertificatePath)
	if err != nil {
		return Credential{}, err
	}
	defer certificate.Close()

	startValidity := c.clock.Now()
	creds, err := c.generateCreds(logger, container, certificate, instanceKey, startValidity, startValidity.Add(c.validityPeriod))
	if err != nil {
		return Credential{}, err
	}

	err = instanceKey.Close()
	if err != nil {
		return Credential{}, err
	}

	err = certificate.Close()
	if err != nil {
		return Credential{}, err
	}

	err = os.Rename(tmpInstanceKeyPath, instanceKeyPath)
	if err != nil {
		return Credential{}, err
	}

	err = os.Rename(tmpCertificatePath, certificatePath)
	if err != nil {
		return Credential{}, err
	}

	return creds, nil
}

func (c *credManager) generateCreds(logger lager.Logger, container executor.Container, certificate io.Writer, instanceKey io.Writer, startValidity time.Time, endValidity time.Time) (Credential, error) {
	logger = logger.Session("generating-credentials")
	logger.Info("starting")
	defer logger.Info("complete")

	logger.Debug("generating-private-key")
	privateKey, err := rsa.GenerateKey(c.entropyReader, 2048)
	if err != nil {
		return Credential{}, err
	}
	logger.Debug("generated-private-key")

	ipForCert := container.InternalIP
	if len(ipForCert) == 0 {
		ipForCert = container.ExternalIP
	}
	template := createCertificateTemplate(ipForCert,
		container.Guid,
		startValidity,
		endValidity,
		container.CertificateProperties.OrganizationalUnit,
	)

	logger.Debug("generating-serial-number")
	guid, err := uuid.NewV4()
	if err != nil {
		logger.Error("failed-to-generate-uuid", err)
		return Credential{}, err
	}
	logger.Debug("generated-serial-number")

	guidBytes := [16]byte(*guid)
	template.SerialNumber.SetBytes(guidBytes[:])

	logger.Debug("generating-certificate")
	certBytes, err := x509.CreateCertificate(c.entropyReader, template, c.CaCert, privateKey.Public(), c.privateKey)
	if err != nil {
		return Credential{}, err
	}
	logger.Debug("generated-certificate")

	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)

	var keyBuf bytes.Buffer
	err = pemEncode(privateKeyBytes, privateKeyPEMBlockType, io.MultiWriter(instanceKey, &keyBuf))
	if err != nil {
		return Credential{}, err
	}

	var certificateBuf bytes.Buffer
	certificateWriter := io.MultiWriter(certificate, &certificateBuf)
	err = pemEncode(certBytes, certificatePEMBlockType, certificateWriter)
	if err != nil {
		return Credential{}, err
	}

	err = pemEncode(c.CaCert.Raw, certificatePEMBlockType, certificateWriter)
	if err != nil {
		return Credential{}, err
	}

	creds := Credential{
		Cert: certificateBuf.String(),
		Key:  keyBuf.String(),
	}
	return creds, nil
}

func (c *credManager) RemoveCredDir(logger lager.Logger, container executor.Container) error {
	logger = logger.Session("remove-credentials")
	logger.Info("starting")
	defer logger.Info("complete")

	return os.RemoveAll(filepath.Join(c.credDir, container.Guid))
}

func pemEncode(bytes []byte, blockType string, writer io.Writer) error {
	block := &pem.Block{
		Type:  blockType,
		Bytes: bytes,
	}
	return pem.Encode(writer, block)
}

func createCertificateTemplate(ipaddress, guid string, notBefore, notAfter time.Time, organizationalUnits []string) *x509.Certificate {
	var ipaddr []net.IP
	if len(ipaddress) == 0 {
		ipaddr = []net.IP{}
	} else {
		ipaddr = []net.IP{net.ParseIP(ipaddress)}
	}
	return &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			CommonName:         guid,
			OrganizationalUnit: organizationalUnits,
		},
		IPAddresses: ipaddr,
		DNSNames:    []string{guid},
		NotBefore:   notBefore,
		NotAfter:    notAfter,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment | x509.KeyUsageKeyAgreement,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
	}
}

func certFromFile(certFile string) (*x509.Certificate, error) {
	data, err := ioutil.ReadFile(certFile)
	if err != nil {
		return nil, err
	}
	var block *pem.Block
	block, _ = pem.Decode(data)
	certs, err := x509.ParseCertificates(block.Bytes)
	if err != nil {
		return nil, err
	}
	cert := certs[0]
	return cert, nil
}
