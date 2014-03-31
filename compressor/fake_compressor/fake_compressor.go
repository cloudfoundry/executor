package fake_compressor

type FakeCompressor struct {
	Src  string
	Dest string
}

func (compressor *FakeCompressor) Compress(src, dest string) error {
	compressor.Src = src
	compressor.Dest = dest
	return nil
}
