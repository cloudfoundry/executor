package depot

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"io/ioutil"
	"math/big"
	"net"
	"time"

	"code.cloudfoundry.org/lager"
)

type creds struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	template   *x509.Certificate
	cert       []byte
	logger     lager.Logger
}

func NewCredGenerator(logger lager.Logger, template *x509.Certificate) *creds {
	return &creds{
		template: template,
		logger:   logger.Session("generate-certs"),
	}
}

func (c *creds) CreateKeys() error {
	logger := c.logger.Session("create-keys")
	logger.Info("starting")
	defer logger.Info("done")

	var err error
	c.privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	c.publicKey = &c.privateKey.PublicKey
	return nil
}

func (c *creds) CreateCert() error {
	logger := c.logger.Session("create-certs")
	logger.Info("starting")
	defer logger.Info("done")

	var parent = c.template
	var err error
	c.cert, err = x509.CreateCertificate(rand.Reader, c.template, parent, c.publicKey, c.privateKey)
	if err != nil {
		return err
	}
	return nil
}

func (c *creds) Save() error {
	logger := c.logger.Session("save-keys")
	logger.Info("starting")
	defer logger.Info("done")
	pkey := x509.MarshalPKCS1PrivateKey(c.privateKey)
	file, err := ioutil.TempFile("", "private")
	if err != nil {
		return err
	}
	_, err = file.Write(pkey)
	if err != nil {
		return err
	}

	// pubkey, _ := x509.MarshalPKIXPublicKey(c.publicKey)
	// file, err := ioutil.TempFile("", "pub")
	// if err != nil {
	// 	return err
	// }
	// _, err = file.Write(pubkey)
	// if err != nil {
	// 	return err
	// }

	file, err = ioutil.TempFile("", "cert")
	if err != nil {
		return err
	}
	_, err = file.Write(c.cert)
	if err != nil {
		return err
	}

	return nil
}

func createCertificateTemplate(ipaddress string) *x509.Certificate {
	return &x509.Certificate{
		IsCA: true,
		BasicConstraintsValid: true,
		SubjectKeyId:          []byte{1, 2, 3},
		SerialNumber:          big.NewInt(1234),
		Subject: pkix.Name{
			Country:      []string{"US"},
			Organization: []string{"CloudFoundry"},
		},
		IPAddresses: []net.IP{net.ParseIP(ipaddress)},
		NotBefore:   time.Now(),
		NotAfter:    time.Now().AddDate(5, 5, 5),
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}
}
