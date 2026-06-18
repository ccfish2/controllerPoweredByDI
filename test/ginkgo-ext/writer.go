package ginkgoext

import (
	"bytes"
	"io"

	"github.com/ccfish2/infra/pkg/lock"
)

type Writer struct {
	Buffer    *bytes.Buffer
	outWriter io.Writer
	lock      *lock.Mutex
}

func NewWriter(out io.Writer) *Writer {
	return &Writer{
		Buffer:    &bytes.Buffer{},
		outWriter: out,
		lock:      &lock.Mutex{},
	}
}

// append contents to buffer and writer
// if too large, the writer will panic with too large error
func (w *Writer) Write(b []byte) (n int, err error) {
	w.lock.Lock()
	defer w.lock.Unlock()

	n, err = w.Buffer.Write(b)
	if err != nil {
		return n, err
	}

	if w.outWriter != nil {
		_, err = w.outWriter.Write(b)
		if err != nil {
			return n, err
		}
	}

	return n, nil
}

func (w *Writer) Reset() {
	w.lock.Lock()
	defer w.lock.Unlock()

	w.Buffer.Reset()
}

func (w *Writer) Bytes() []byte {
	w.lock.Lock()
	defer w.lock.Unlock()

	return w.Buffer.Bytes()
}
