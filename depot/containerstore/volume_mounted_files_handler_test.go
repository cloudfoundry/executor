package containerstore_test

import (
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

		handler   *containerstore.VolumeMountedFilesHandler
		container executor.Container
	)

	AfterEach(func() {
		os.RemoveAll(tmpdir)
	})

	BeforeEach(func() {
		fakeContainerUUID = "E62613F8-7E85-4F49-B3EF-690BD2AE7EF2"

		tmpdir = filepath.Join(os.TempDir(), "volume_mounted_files")
		err := os.MkdirAll(tmpdir, os.ModePerm)
		Expect(err).NotTo(HaveOccurred())

		handler = containerstore.NewVolumeMountedFilesHandler(
			tmpdir,
			fakeContainerUUID,
		)
	})

	Context("CreateDir", func() {
		It("returns a valid bind mount", func() {
			container = executor.Container{Guid: fakeContainerUUID}

			container.VolumeMountedFiles = append(container.VolumeMountedFiles, executor.VolumeMountedFiles{
				Path: "/redis/username", Content: "username",
			})

			container.VolumeMountedFiles = append(container.VolumeMountedFiles, executor.VolumeMountedFiles{
				Path: "/redis/password", Content: "password",
			})

			container.VolumeMountedFiles = append(container.VolumeMountedFiles, executor.VolumeMountedFiles{
				Path: "/httpd/password", Content: "password",
			})

			container.VolumeMountedFiles = append(container.VolumeMountedFiles, executor.VolumeMountedFiles{
				Path: "/httpd/username", Content: "username",
			})

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

			Expect(filepath.Join(tmpdir, fakeContainerUUID, "redis", "password")).To(BeAnExistingFile())
			Expect(filepath.Join(tmpdir, fakeContainerUUID, "redis", "password")).To(BeARegularFile())

			Expect(filepath.Join(tmpdir, fakeContainerUUID, "httpd", "username")).To(BeAnExistingFile())
			Expect(filepath.Join(tmpdir, fakeContainerUUID, "httpd", "username")).To(BeARegularFile())

			Expect(filepath.Join(tmpdir, fakeContainerUUID, "httpd", "password")).To(BeAnExistingFile())
			Expect(filepath.Join(tmpdir, fakeContainerUUID, "httpd", "password")).To(BeARegularFile())
		})

		It("validates the content of the redis username file", func() {
			_, err := handler.CreateDir(logger, container)
			Expect(err).To(Succeed())

			usernameFilePath := filepath.Join(tmpdir, fakeContainerUUID, "redis", "username")
			Expect(usernameFilePath).To(BeAnExistingFile())

			content, err := os.ReadFile(usernameFilePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("username"))
		})

		It("validates the content of the redis password file", func() {
			_, err := handler.CreateDir(logger, container)
			Expect(err).To(Succeed())

			usernameFilePath := filepath.Join(tmpdir, fakeContainerUUID, "redis", "password")
			Expect(usernameFilePath).To(BeAnExistingFile())

			content, err := os.ReadFile(usernameFilePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("password"))
		})

		It("validates the content of the httpd username file", func() {
			_, err := handler.CreateDir(logger, container)
			Expect(err).To(Succeed())

			usernameFilePath := filepath.Join(tmpdir, fakeContainerUUID, "httpd", "username")
			Expect(usernameFilePath).To(BeAnExistingFile())

			content, err := os.ReadFile(usernameFilePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("username"))
		})

		It("validates the content of the httpd password file", func() {
			_, err := handler.CreateDir(logger, container)
			Expect(err).To(Succeed())

			usernameFilePath := filepath.Join(tmpdir, fakeContainerUUID, "httpd", "password")
			Expect(usernameFilePath).To(BeAnExistingFile())

			content, err := os.ReadFile(usernameFilePath)
			Expect(err).NotTo(HaveOccurred())
			Expect(string(content)).To(Equal("password"))
		})

		It("removes volume-mounted-files directory", func() {
			err := handler.RemoveDir(logger, container)
			Expect(err).NotTo(HaveOccurred())

			volumeMountedFiles := filepath.Join(tmpdir, fakeContainerUUID)
			Eventually(volumeMountedFiles).ShouldNot(BeADirectory())
		})

		Context("when making directory fails", func() {
			BeforeEach(func() {
				handler = containerstore.NewVolumeMountedFilesHandler(
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
