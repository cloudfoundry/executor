package containerstore

import (
	"fmt"
	"net/url"

	"github.com/cloudfoundry-incubator/cacheddownloader"
	"github.com/cloudfoundry-incubator/executor"
	"github.com/cloudfoundry-incubator/executor/depot/log_streamer"
	"github.com/cloudfoundry-incubator/garden"
	"github.com/pivotal-golang/bytefmt"
	"github.com/pivotal-golang/lager"
)

//go:generate counterfeiter -o containerstorefakes/fake_bindmounter.go . DependencyManager

type DependencyManager interface {
	DownloadCachedDependencies(logger lager.Logger, mounts []executor.CachedDependency, logStreamer log_streamer.LogStreamer) (BindMounts, error)
	ReleaseCachedDependencies(logger lager.Logger, keys []BindMountCacheKey) error
}

type dependencyManager struct {
	cache cacheddownloader.CachedDownloader
}

func NewDependencyManager(cache cacheddownloader.CachedDownloader) DependencyManager {
	return &dependencyManager{cache}
}

func (bm *dependencyManager) DownloadCachedDependencies(logger lager.Logger, mounts []executor.CachedDependency, streamer log_streamer.LogStreamer) (BindMounts, error) {
	bindMounts := NewBindMounts(len(mounts))

	for i := range mounts {
		mount := &mounts[i]
		emit(streamer, "Downloading %s...\n", mount.Name)

		downloadURL, err := url.Parse(mount.From)
		if err != nil {
			logger.Error("failed-parsing-bind-mount-download-url", err, lager.Data{"download-url": mount.From, "cache-key": mount.CacheKey})
			emit(streamer, "Downloading %s failed", mount.Name)
			return BindMounts{}, err
		}

		logger.Debug("fetching-cache-dependency", lager.Data{"download-url": downloadURL.String(), "cache-key": mount.CacheKey})
		dirPath, downloadedSize, err := bm.cache.FetchAsDirectory(downloadURL, mount.CacheKey, nil)
		if err != nil {
			logger.Error("failed-fetching-cache-dependency", err, lager.Data{"download-url": downloadURL.String(), "cache-key": mount.CacheKey})
			emit(streamer, "Downloading %s failed", mount.Name)
			return BindMounts{}, err
		}

		if downloadedSize != 0 {
			emit(streamer, "Downloaded %s (%s)\n", mount.Name, bytefmt.ByteSize(uint64(downloadedSize)))
		} else {
			emit(streamer, "Downloaded %s\n", mount.Name)
		}

		bindMounts.AddBindMount(mount.CacheKey, newBindMount(dirPath, mount.To))
	}

	return bindMounts, nil
}

func (bm *dependencyManager) ReleaseCachedDependencies(logger lager.Logger, keys []BindMountCacheKey) error {
	for i := range keys {
		key := &keys[i]
		logger.Debug("releasing-cache-key", lager.Data{"cache-key": key.CacheKey, "dir": key.Dir})
		err := bm.cache.CloseDirectory(key.CacheKey, key.Dir)
		if err != nil {
			logger.Error("failed-releasing-cache-key", err, lager.Data{"cache-key": key.CacheKey, "dir": key.Dir})
			return err
		}
	}
	return nil
}

func emit(streamer log_streamer.LogStreamer, format string, a ...interface{}) {
	fmt.Fprintf(streamer.Stdout(), format, a...)
}

type BindMounts struct {
	CacheKeys        []BindMountCacheKey
	GardenBindMounts []garden.BindMount
}

func NewBindMounts(capacity int) BindMounts {
	return BindMounts{
		CacheKeys:        make([]BindMountCacheKey, 0, capacity),
		GardenBindMounts: make([]garden.BindMount, 0, capacity),
	}
}

func (b *BindMounts) AddBindMount(cacheKey string, mount garden.BindMount) {
	b.CacheKeys = append(b.CacheKeys, NewbindMountCacheKey(cacheKey, mount.SrcPath))
	b.GardenBindMounts = append(b.GardenBindMounts, mount)
}

type BindMountCacheKey struct {
	CacheKey string
	Dir      string
}

func NewbindMountCacheKey(cacheKey, dir string) BindMountCacheKey {
	return BindMountCacheKey{CacheKey: cacheKey, Dir: dir}
}
