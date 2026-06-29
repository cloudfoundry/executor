package containerstore_test

import (
	"os"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"code.cloudfoundry.org/executor"
	"code.cloudfoundry.org/executor/depot/containerstore"
	"code.cloudfoundry.org/garden"
)

var _ = Describe("SpiffeSocketHandler", func() {
	var (
		tmpdir        string
		hostSocketDir string
		handler       *containerstore.SpiffeSocketHandler
		container     executor.Container
	)

	BeforeEach(func() {
		var err error
		tmpdir, err = os.MkdirTemp("", "spiffe-socket")
		Expect(err).NotTo(HaveOccurred())
		// Point at a path that does NOT exist: the handler must never create it.
		hostSocketDir = tmpdir + "/does-not-exist"
		container = executor.Container{Guid: "some-guid"}
		handler = containerstore.NewSpiffeSocketHandler(hostSocketDir)
	})

	AfterEach(func() {
		os.RemoveAll(tmpdir)
	})

	Context("CreateDir", func() {
		It("returns exactly one read-only host bind mount of the socket dir", func() {
			mounts, _, err := handler.CreateDir(logger, container)
			Expect(err).NotTo(HaveOccurred())

			Expect(mounts).To(HaveLen(1))
			Expect(mounts[0].SrcPath).To(Equal(hostSocketDir))
			Expect(mounts[0].DstPath).To(Equal("/run/spiffe"))
			Expect(mounts[0].Mode).To(Equal(garden.BindMountModeRO))
			Expect(mounts[0].Origin).To(Equal(garden.BindMountOriginHost))
		})

		It("returns exactly one SPIFFE_ENDPOINT_SOCKET env var", func() {
			_, envs, err := handler.CreateDir(logger, container)
			Expect(err).NotTo(HaveOccurred())

			Expect(envs).To(HaveLen(1))
			Expect(envs[0].Name).To(Equal("SPIFFE_ENDPOINT_SOCKET"))
			Expect(envs[0].Value).To(Equal("unix:///run/spiffe/workload.sock"))
		})

		It("creates nothing on disk", func() {
			_, _, err := handler.CreateDir(logger, container)
			Expect(err).NotTo(HaveOccurred())

			entries, err := os.ReadDir(tmpdir)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})
	})

	Context("no-op methods", func() {
		AfterEach(func() {
			entries, err := os.ReadDir(tmpdir)
			Expect(err).NotTo(HaveOccurred())
			Expect(entries).To(BeEmpty())
		})

		It("RemoveDir returns nil and touches nothing", func() {
			Expect(handler.RemoveDir(logger, container)).To(BeNil())
		})

		It("Update with empty Credentials returns nil and touches nothing", func() {
			Expect(handler.Update(containerstore.Credentials{}, container)).To(BeNil())
		})

		It("Update with non-empty Credentials returns nil and touches nothing", func() {
			creds := containerstore.Credentials{
				InstanceIdentityCredential: containerstore.Credential{Cert: "cert", Key: "key"},
			}
			Expect(handler.Update(creds, container)).To(BeNil())
		})

		It("Close returns nil and touches nothing", func() {
			Expect(handler.Close(containerstore.Credentials{}, container)).To(BeNil())
		})
	})
})
