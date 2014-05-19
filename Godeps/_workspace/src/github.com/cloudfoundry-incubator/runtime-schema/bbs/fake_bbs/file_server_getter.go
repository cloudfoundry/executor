package fake_bbs

type FileServerGetter struct {
	WhenGettingAvailableFileServer func() (string, error)
}

func (fs *FileServerGetter) GetAvailableFileServer() (string, error) {
	if fs.WhenGettingAvailableFileServer != nil {
		return fs.WhenGettingAvailableFileServer()
	}

	return "http://some-fake-file-server", nil
}
