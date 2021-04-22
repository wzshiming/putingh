package putingh

import (
	"io"
)

func newReaderWithAutoCloser(rc io.ReadCloser) io.Reader {
	return &readerWithAutoCloser{
		rc: rc,
	}
}

type readerWithAutoCloser struct {
	rc io.ReadCloser
}

func (r *readerWithAutoCloser) Read(p []byte) (n int, err error) {
	n, err = r.rc.Read(p)
	if err == io.EOF {
		r.rc.Close()
	}
	return n, err
}
