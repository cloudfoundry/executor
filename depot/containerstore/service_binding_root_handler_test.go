package containerstore_test

import (
	"io/ioutil"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/garden"
)

var _ = Describe("Service Binding Root Handler", func() {
	var (
		tmpdir            string
		fakeContainerUUID string

		handler   *containerstore.ServiceBindingRootHandler
		container executor.Container
	)

	AfterEach(func() {
		os.RemoveAll(tmpdir)
	})

	BeforeEach(func() {
		fakeContainerUUID = "E62613F8-7E85-4F49-B3EF-690BD2AE7EF2"

		container = executor.Container{Guid: fakeContainerUUID}

		var filesVars []executor.FileBasedServiceBinding
		container.FileBasedServiceBindings = append(filesVars, executor.FileBasedServiceBinding{
			Name: "/redis/username", Value: "username",
		})

		tmpdir = filepath.Join(os.TempDir(), "service_binding_root_handler")
		err := os.MkdirAll(tmpdir, os.ModePerm)
		Expect(err).NotTo(HaveOccurred())

		handler = containerstore.NewServiceBindingRootHandler(
			tmpdir,
			fakeContainerUUID,
		)
	})

	Context("CreateDir", func() {
		It("returns a valid bind mount", func() {
			mount, err := handler.CreateDir(logger, container)
			Expect(err).To(Succeed())

			Expect(mount).To(HaveLen(1))
			Expect(mount[0].SrcPath).To(BeADirectory())
			Expect(mount[0].DstPath).To(Equal(fakeContainerUUID))
			Expect(mount[0].Mode).To(Equal(garden.BindMountModeRO))
			Expect(mount[0].Origin).To(Equal(garden.BindMountOriginHost))
		})

		It("returns a valid service configuration directory", func() {
			_, err := handler.CreateDir(logger, container)
			Expect(err).To(Succeed())

			Expect(filepath.Join(tmpdir, fakeContainerUUID, "redis")).To(BeADirectory())
			Expect(filepath.Join(tmpdir, fakeContainerUUID, "redis", "username")).To(BeAnExistingFile())
			Expect(filepath.Join(tmpdir, fakeContainerUUID, "redis", "username")).To(BeARegularFile())
		})

		It("validates the content of the username file", func() {
			_, err := handler.CreateDir(logger, container)
			Expect(err).To(Succeed())

			usernameFilePath := filepath.Join(tmpdir, fakeContainerUUID, "redis", "username")
			Expect(usernameFilePath).To(BeAnExistingFile())

			content, err := ioutil.ReadFile(usernameFilePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("username"))
		})

		It("removes service binding root directory", func() {
			err := handler.RemoveDir(logger, container)
			Expect(err).NotTo(HaveOccurred())

			serviceBindingRootPath := filepath.Join(tmpdir, fakeContainerUUID)
			Eventually(serviceBindingRootPath).ShouldNot(BeADirectory())
		})

		Context("when making directory fails", func() {
			BeforeEach(func() {
				handler = containerstore.NewServiceBindingRootHandler(
					"/some/fake/path",
					"mount_path",
				)
			})

			It("returns an error", func() {
				_, err := handler.CreateDir(logger, executor.Container{Guid: "fake_guid"})
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
