package containerstore_test

import (
	"errors"
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/executor/depot/containerstore/containerstorefakes"
)

var _ = Describe("InstanceIdentityHandler", func() {
	var (
		tmpdir          string
		handler         *containerstore.InstanceIdentityHandler
		container       executor.Container
		containerWriter *containerstorefakes.FakeContainerWriter
	)

	AfterEach(func() {
		os.RemoveAll(tmpdir)
	})

	BeforeEach(func() {
		container = executor.Container{Guid: "some-guid"}
		var err error
		tmpdir, err = ioutil.TempDir("", "credsmanager")
		Expect(err).NotTo(HaveOccurred())
		containerWriter = new(containerstorefakes.FakeContainerWriter)
		handler = containerstore.NewInstanceIdentityHandler(
			tmpdir,
			"containerpath",
			containerWriter,
		)
	})

	Context("CreateDir", func() {
		It("returns CF_INSTANCE_CERT and CF_INSTANCE_KEY environment variable values", func() {
			containerConfiguration, err := handler.CreateDir(logger, container)
			Expect(err).To(Succeed())
			envVariables := containerConfiguration.Env

			Expect(envVariables).To(HaveLen(2))
			values := map[string]string{}
			values[envVariables[0].Name] = envVariables[0].Value
			values[envVariables[1].Name] = envVariables[1].Value
			Expect(values).To(Equal(map[string]string{
				"CF_INSTANCE_CERT": "containerpath/instance.crt",
				"CF_INSTANCE_KEY":  "containerpath/instance.key",
			}))
		})

		It("returns the container tmpfs", func() {
			containerConfiguration, err := handler.CreateDir(logger, container)
			Expect(err).To(Succeed())
			tmpfs := containerConfiguration.Tmpfs

			Expect(tmpfs).To(HaveLen(1))
			Expect(tmpfs[0].Path).To(Equal("containerpath"))
		})

		Context("when making directory fails", func() {
			BeforeEach(func() {
				handler = containerstore.NewInstanceIdentityHandler(
					"/invalid/path",
					"containerpath",
					containerWriter,
				)
			})

			It("returns an error", func() {
				_, err := handler.CreateDir(logger, executor.Container{Guid: "somefailure"})
				Expect(err).To(HaveOccurred())
			})
		})
	})

	Describe("Update", func() {
		BeforeEach(func() {
			_, err := handler.CreateDir(logger, container)
			Expect(err).To(BeNil())
		})

		It("puts private key into container directory", func() {
			err := handler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
			Expect(err).NotTo(HaveOccurred())

			certPath := filepath.Join(tmpdir, "some-guid")
			keyFile := filepath.Join(certPath, "instance.key")
			Expect(keyFile).To(BeARegularFile())

			data, err := ioutil.ReadFile(keyFile)
			Expect(err).NotTo(HaveOccurred())

			Expect(string(data)).To(Equal("key"))
		})

		It("puts the certificate into container directory", func() {
			err := handler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
			Expect(err).NotTo(HaveOccurred())

			certPath := filepath.Join(tmpdir, "some-guid")
			certFile := filepath.Join(certPath, "instance.crt")
			Expect(certFile).To(BeARegularFile())

			data, err := ioutil.ReadFile(certFile)
			Expect(err).NotTo(HaveOccurred())

			Expect(string(data)).To(Equal("cert"))
		})

		It("writes private key into the container", func() {
			err := handler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerWriter.WriteFileCallCount()).To(Equal(2))

			actualContainerID, actualPath, actualContent := containerWriter.WriteFileArgsForCall(0)
			Expect(actualContainerID).To(Equal("some-guid"))
			Expect(actualPath).To(Equal("containerpath/instance.key"))
			Expect(string(actualContent)).To(Equal("key"))
		})

		It("writes the certificate into the container", func() {
			err := handler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
			Expect(err).NotTo(HaveOccurred())
			Expect(containerWriter.WriteFileCallCount()).To(Equal(2))

			actualContainerID, actualPath, actualContent := containerWriter.WriteFileArgsForCall(1)
			Expect(actualContainerID).To(Equal("some-guid"))
			Expect(actualPath).To(Equal("containerpath/instance.crt"))
			Expect(string(actualContent)).To(Equal("cert"))
		})

		Context("when writing the key to the container fails", func() {
			BeforeEach(func() {
				containerWriter.WriteFileReturnsOnCall(0, errors.New("write-key-failure"))
			})

			It("returns the error", func() {
				err := handler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).To(MatchError("write-key-failure"))
			})
		})

		Context("when writing the certificate to the container fails", func() {
			BeforeEach(func() {
				containerWriter.WriteFileReturnsOnCall(1, errors.New("write-cert-failure"))
			})

			It("returns the error", func() {
				err := handler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
				Expect(err).To(MatchError("write-cert-failure"))
			})
		})
	})

	Describe("Close", func() {
		BeforeEach(func() {
			_, err := handler.CreateDir(logger, container)
			Expect(err).To(BeNil())
			err = handler.Update(containerstore.Credential{Cert: "cert", Key: "key"}, container)
			Expect(err).NotTo(HaveOccurred())
		})

		It("does not put private key into container directory", func() {
			err := handler.Close(containerstore.Credential{Cert: "invalid-cert", Key: "invalid-key"}, container)
			Expect(err).NotTo(HaveOccurred())

			certPath := filepath.Join(tmpdir, "some-guid")
			keyFile := filepath.Join(certPath, "instance.key")
			Expect(keyFile).To(BeARegularFile())

			data, err := ioutil.ReadFile(keyFile)
			Expect(err).NotTo(HaveOccurred())

			Expect(string(data)).NotTo(Equal("invalid-key"))
		})

		It("does not put the certificate into container directory", func() {
			err := handler.Close(containerstore.Credential{Cert: "invalid-cert", Key: "invalid-key"}, container)
			Expect(err).NotTo(HaveOccurred())

			certPath := filepath.Join(tmpdir, "some-guid")
			certFile := filepath.Join(certPath, "instance.crt")
			Expect(certFile).To(BeARegularFile())

			data, err := ioutil.ReadFile(certFile)
			Expect(err).NotTo(HaveOccurred())

			Expect(string(data)).NotTo(Equal("invalid-cert"))
		})
	})

	Context("RemoveDir", func() {
		BeforeEach(func() {
			_, err := handler.CreateDir(logger, container)
			Expect(err).NotTo(HaveOccurred())
			certPath := filepath.Join(tmpdir, "some-guid")
			Eventually(certPath).Should(BeADirectory())
		})

		It("removes the directory created", func() {
			err := handler.RemoveDir(logger, container)
			Expect(err).NotTo(HaveOccurred())
			certPath := filepath.Join(tmpdir, "some-guid")
			Eventually(certPath).ShouldNot(BeADirectory())
		})
	})
})
