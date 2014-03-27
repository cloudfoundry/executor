package fake_extractor

type FakeExtractor struct {
	ExtractedFilePaths []string
}

func (extractor *FakeExtractor) Extract(src, dest string) error {
	extractor.ExtractedFilePaths = append(extractor.ExtractedFilePaths, src)
	return nil
}
