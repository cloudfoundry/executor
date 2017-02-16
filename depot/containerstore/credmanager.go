package containerstore

import (
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"os"
	"path"
	"path/filepath"
	"time"

	"code.cloudfoundry.org/clock"
	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/garden"
	"code.cloudfoundry.org/lager"
)

//go:generate counterfeiter -o containerstorefakes/fake_cred_manager.go . CredManager
type CredManager interface {
	CreateCredDir(lager.Logger, executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error)
	GenerateCreds(lager.Logger, executor.Container) error
	RemoveCreds(lager.Logger, executor.Container) error
}

type noopManager struct{}

func NewNoopCredManager() CredManager {
	return &noopManager{}
}

func (c *noopManager) CreateCredDir(logger lager.Logger, container executor.Container) ([]garden.BindMount, []executor.EnvironmentVariable, error) {
	return nil, nil, nil
}
func (c *noopManager) GenerateCreds(logger lager.Logger, container executor.Container) error {
	return nil
}
func (c *noopManager) RemoveCreds(logger lager.Logger, container executor.Container) error {
	return nil
}

type credManager struct {
	credDir            string
	entropyReader      io.Reader
	clock              clock.Clock
	CaCert             *x509.Certificate
	privateKey         *rsa.PrivateKey
	containerMountPath string
}

func NewCredManager(
	credDir string,
	entropyReader io.Reader,
	clock clock.Clock,
	CaCert *x509.Certificate,
	privateKey *rsa.PrivateKey,
	containerMountPath string,
) CredManager {
	return &credManager{
		credDir:            credDir,
		entropyReader:      entropyReader,
		clock:              clock,
		CaCert:             CaCert,
		privateKey:         privateKey,
		containerMountPath: containerMountPath,
	}
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

func (c *credManager) GenerateCreds(logger lager.Logger, container executor.Container) error {
	logger = logger.Session("generating-credentials")
	logger.Info("starting")
	defer logger.Info("complete")

	privateKey, err := rsa.GenerateKey(c.entropyReader, 2048)
	if err != nil {
		return err
	}

	template := createCertificateTemplate(container.InternalIP, container.Guid, c.clock.Now(), c.clock.Now().Add(24*time.Hour), container.OrganizationalUnits)
	template.SerialNumber.SetBytes([]byte(container.Guid))

	certBytes, err := x509.CreateCertificate(c.entropyReader, template, c.CaCert, privateKey.Public(), c.privateKey)
	if err != nil {
		return err
	}

	instanceKeyPath := filepath.Join(c.credDir, container.Guid, "instance.key")
	privateKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	instanceKey, err := os.Create(instanceKeyPath)
	if err != nil {
		return err
	}

	defer instanceKey.Close()

	err = pemEncode(privateKeyBytes, privateKeyPEMBlockType, instanceKey)
	if err != nil {
		return err
	}

	certificatePath := filepath.Join(c.credDir, container.Guid, "instance.crt")
	certificate, err := os.Create(certificatePath)
	if err != nil {
		return err
	}

	defer certificate.Close()
	err = pemEncode(certBytes, certificatePEMBlockType, certificate)
	if err != nil {
		return err
	}

	return pemEncode(c.CaCert.Raw, certificatePEMBlockType, certificate)
}

func (c *credManager) RemoveCreds(logger lager.Logger, container executor.Container) error {
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
	return &x509.Certificate{
		SerialNumber: big.NewInt(0),
		Subject: pkix.Name{
			CommonName:         guid,
			OrganizationalUnit: organizationalUnits,
		},
		IPAddresses: []net.IP{net.ParseIP(ipaddress)},
		NotBefore:   notBefore,
		NotAfter:    notAfter,
	}
}
