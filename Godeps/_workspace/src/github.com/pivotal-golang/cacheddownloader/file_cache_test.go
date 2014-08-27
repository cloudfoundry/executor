package cacheddownloader_test

import (
	"io"
	"io/ioutil"
	"os"
	"runtime"

	. "github.com/pivotal-golang/cacheddownloader"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("FileCache", func() {
	var cache *FileCache
	var cacheDir string
	var err error

	BeforeEach(func() {
		cacheDir, err = ioutil.TempDir("", "cache-test")
		Ω(err).ShouldNot(HaveOccurred())

		cache = NewCache(cacheDir, 123424)
	})

	AfterEach(func() {
		os.RemoveAll(cacheDir)
	})

	Describe("adding a file to the cache", func() {
		var sourceFile *os.File
		var cacheReader io.ReadCloser

		BeforeEach(func() {
			sourceFile, err = ioutil.TempFile("", "cache-test-file")
			Ω(err).ShouldNot(HaveOccurred())
			sourceFile.WriteString("the-file-content")
			sourceFile.Close()

			added, err := cache.Add("the-cache-key", sourceFile.Name(), 100, CachingInfoType{})
			Ω(err).ShouldNot(HaveOccurred())
			Ω(added).Should(BeTrue())

			cacheReader, err = cache.Get("the-cache-key")
			Ω(err).ShouldNot(HaveOccurred())
		})

		AfterEach(func() {
			sourceFile.Close()
			os.RemoveAll(sourceFile.Name())
		})

		It("makes the file available for subsequent reads", func() {
			content, err := ioutil.ReadAll(cacheReader)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(string(content)).Should(Equal("the-file-content"))
		})

		Describe("when a new file is added for the same cache key", func() {
			var newSourceFile *os.File

			BeforeEach(func() {
				newSourceFile, err = ioutil.TempFile("", "cache-test-file")
				Ω(err).ShouldNot(HaveOccurred())
				newSourceFile.WriteString("new-file-content")
				newSourceFile.Close()
			})

			AfterEach(func() {
				newSourceFile.Close()
				os.RemoveAll(sourceFile.Name())
			})

			It("still allows existing readers to be read", func() {
				_, err := cache.Add("the-cache-key", newSourceFile.Name(), 100, CachingInfoType{})
				Ω(err).ShouldNot(HaveOccurred())

				content, err := ioutil.ReadAll(cacheReader)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(content)).Should(Equal("the-file-content"))
			})

			It("returns the new file for subsequent reads", func() {
				_, err := cache.Add("the-cache-key", newSourceFile.Name(), 100, CachingInfoType{})
				Ω(err).ShouldNot(HaveOccurred())

				newCacheReader, err := cache.Get("the-cache-key")
				Ω(err).ShouldNot(HaveOccurred())

				content, err := ioutil.ReadAll(newCacheReader)
				Ω(err).ShouldNot(HaveOccurred())
				Ω(string(content)).Should(Equal("new-file-content"))
			})

			It("stores files in the cache with unique names so they don't need to be replaced on windows", func() {
				cachedFilenamesBeforeReplace := filenamesInDir(cacheDir)
				_, err := cache.Add("the-cache-key", newSourceFile.Name(), 100, CachingInfoType{})
				Ω(err).ShouldNot(HaveOccurred())

				cachedFilenamesAfterReplace := filenamesInDir(cacheDir)
				Ω(cachedFilenamesBeforeReplace).ShouldNot(Equal(cachedFilenamesAfterReplace))
			})

			It("only deletes the old file when the readers are closed", func() {
				_, err := cache.Add("the-cache-key", newSourceFile.Name(), 100, CachingInfoType{})
				Ω(err).ShouldNot(HaveOccurred())

				newCacheReader, err := cache.Get("the-cache-key")
				Ω(err).ShouldNot(HaveOccurred())

				if runtime.GOOS == "windows" {
					// the file cannot be deleted while its reader is open
					Ω(filenamesInDir(cacheDir)).Should(HaveLen(2))

				} else {
					Ω(filenamesInDir(cacheDir)).Should(HaveLen(1))
				}

				cacheReader.Close()
				Ω(filenamesInDir(cacheDir)).Should(HaveLen(1))

				newCacheReader.Close()
				Ω(filenamesInDir(cacheDir)).Should(HaveLen(1))
			})
		})
	})
})

func filenamesInDir(dir string) []string {
	entries, err := ioutil.ReadDir(dir)
	Ω(err).ShouldNot(HaveOccurred())

	result := []string{}
	for _, entry := range entries {
		result = append(result, entry.Name())
	}

	return result
}
