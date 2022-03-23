package gonl

import (
	"bytes"
	_ "embed"
	"errors"
	"io"
	"testing"
)

// bufSize will be used when making buffers for copying byte slices,
// and its size is chosen based on what io.Copy will create when given
// an empty slice.
const bufSize = 32 * 1024

//go:embed 2600-0.txt
var novel []byte

// copyBuffer is a modified version of similarly named function in
// standard library, provided here to be able to test using
// io.CopyBuffer both with and without sending it a destination
// structure that implements io.ReaderFrom.
func copyBuffer(dst io.Writer, src io.Reader, buf []byte) (int64, error) {
	var written int64
	var err error

	if buf == nil {
		buf = make([]byte, bufSize)
	}

	// See copyBuffer in io standard library.
	for {
		nr, er := src.Read(buf)
		if nr > 0 {
			nw, ew := dst.Write(buf[0:nr])
			if nw < 0 || nr < nw {
				nw = 0
				if ew == nil {
					ew = errors.New("errInvalidWrite")
				}
			}
			written += int64(nw)
			if ew != nil {
				err = ew
				break
			}
			if nr != nw {
				err = io.ErrShortWrite
				break
			}
		}
		if er != nil {
			if er != io.EOF {
				err = er
			}
			break
		}
	}
	return written, err
}

func BenchmarkDiscardWrites(b *testing.B) {
	b.Run("BatchLineLineWriter", func(b *testing.B) {
		// These benchmark functions contrast the benefit of
		// BatchLineWriter having ReadFrom method available rather
		// than only having Write method.
		//
		// For Writes where each call has very little payload or call
		// frequency penalty, the BatchLineWriter can be over twice as
		// fast when ReadFrom is used rather than copying through an
		// additional staging buffer using Write calls alone.

		b.Run("ReadFrom", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				drain := new(discardWriteCloser)

				output, err := NewBatchLineWriter(drain, bufSize)
				if err != nil {
					b.Fatal(err)
				}

				_, err = output.ReadFrom(bytes.NewReader(novel))
				if err != nil {
					b.Fatal(err)
				}

				if err = output.Close(); err != nil {
					b.Fatal(err)
				}

				if got, want := drain.count, len(novel); got != want {
					b.Errorf("GOT: %v; WANT: %v", got, want)
				}
			}
		})
		b.Run("Write", func(b *testing.B) {
			buf := make([]byte, bufSize)

			for i := 0; i < b.N; i++ {
				drain := new(discardWriteCloser)

				output, err := NewBatchLineWriter(drain, bufSize)
				if err != nil {
					b.Fatal(err)
				}

				_, err = copyBuffer(output, bytes.NewReader(novel), buf)
				if err != nil {
					b.Fatal(err)
				}

				if err = output.Close(); err != nil {
					b.Fatal(err)
				}

				if got, want := drain.count, len(novel); got != want {
					b.Errorf("GOT: %v; WANT: %v", got, want)
				}
			}
		})
	})

	b.Run("PerLineWriter", func(b *testing.B) {
		// PerLineWriter is significantly less performant as
		// BatchLineWriter, and it should not be a surprise, as
		// PerLineWriter is optimized for use cases that require a
		// single Write call for each newline terminated line of text,
		// and BatchLineWriter is optimized for streaming newline
		// terminated text, often batching up many tens or even
		// hundreds of lines to be written at once to the underlying
		// io.WriteCloser.
		b.Run("ReadFrom", func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				drain := new(discardWriteCloser)
				output := &PerLineWriter{WC: drain}

				_, err := output.ReadFrom(bytes.NewReader(novel))
				if err != nil {
					b.Fatal(err)
				}

				if err = output.Close(); err != nil {
					b.Fatal(err)
				}

				if got, want := drain.count, len(novel); got != want {
					b.Errorf("GOT: %v; WANT: %v", got, want)
				}
			}
		})
		b.Run("Write", func(b *testing.B) {
			buf := make([]byte, bufSize)

			for i := 0; i < b.N; i++ {
				drain := new(discardWriteCloser)
				output := &PerLineWriter{WC: drain}

				_, err := copyBuffer(output, bytes.NewReader(novel), buf)
				if err != nil {
					b.Fatal(err)
				}

				if err = output.Close(); err != nil {
					b.Fatal(err)
				}

				if got, want := drain.count, len(novel); got != want {
					b.Errorf("GOT: %v; WANT: %v", got, want)
				}
			}
		})
	})
}

func BenchmarkHashWrites(b *testing.B) {
	// ??? not really worried about true message authentication
	// codes. Just want to shove data into an io.Writer that does a
	// bit of work, while also verifying every byte passed through the
	// intermediate structures.
	var key = []byte("this is a dummy key")
	var mac = []byte("\xfav\x96\xd1C\xea\xb4\xdd߿\xd0G\x0e\x95\xa8)\xb5\xed\xe6\x11{e\xf2f\xd2\xea\xf5\xdb=\xb46\xff")

	b.Run("BatchLineLineWriter", func(b *testing.B) {
		// These benchmark functions contrast the benefit of
		// BatchLineWriter having ReadFrom method available rather
		// than only having Write method.
		//
		// For Writes where each call exacts a toll on the process,
		// either proportional to the payload size or the call
		// frequency, the BatchLineWriter is still faster when
		// ReadFrom is used rather than copying through an additional
		// staging buffer using Write calls alone, but the effect is
		// not as dramatic as for scenarios where Writes are less
		// expensive.
		b.Run("ReadFrom", func(b *testing.B) {
			drain := newHashWriteCloser(key)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				output, err := NewBatchLineWriter(drain, bufSize)
				if err != nil {
					b.Fatal(err)
				}

				_, err = output.ReadFrom(bytes.NewReader(novel))
				if err != nil {
					b.Fatal(err)
				}

				if err = output.Close(); err != nil {
					b.Fatal(err)
				}

				if !drain.ValidMAC(mac) {
					b.Errorf("Invalid MAC: %q", drain.MAC())
				}

				drain.Reset()
			}
		})
		b.Run("Write", func(b *testing.B) {
			buf := make([]byte, bufSize)
			drain := newHashWriteCloser(key)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				output, err := NewBatchLineWriter(drain, bufSize)
				if err != nil {
					b.Fatal(err)
				}

				_, err = copyBuffer(output, bytes.NewReader(novel), buf)
				if err != nil {
					b.Fatal(err)
				}

				if err = output.Close(); err != nil {
					b.Fatal(err)
				}

				if !drain.ValidMAC(mac) {
					b.Errorf("Invalid MAC: %q", drain.MAC())
				}

				drain.Reset()
			}
		})
	})

	b.Run("PerLineWriter", func(b *testing.B) {
		// Unsurprisingly, PerLineWriter is significantly less
		// performant in streaming scenarios than BatchLineWriter, but
		// it remains a viable alternative when the use case requires
		// a one-to-one correspondence between newlines and Write
		// calls. That is, each and every line is written individually
		// to the underlying io.WriteCloser.
		b.Run("ReadFrom", func(b *testing.B) {
			drain := newHashWriteCloser(key)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				output := &PerLineWriter{WC: drain}

				_, err := output.ReadFrom(bytes.NewReader(novel))
				if err != nil {
					b.Fatal(err)
				}

				if err = output.Close(); err != nil {
					b.Fatal(err)
				}

				if !drain.ValidMAC(mac) {
					b.Errorf("Invalid MAC: %q", drain.MAC())
				}

				drain.Reset()
			}
		})
		b.Run("Write", func(b *testing.B) {
			buf := make([]byte, bufSize)
			drain := newHashWriteCloser(key)
			b.ResetTimer()

			for i := 0; i < b.N; i++ {
				output := &PerLineWriter{WC: drain}

				_, err := copyBuffer(output, bytes.NewReader(novel), buf)
				if err != nil {
					b.Fatal(err)
				}

				if err = output.Close(); err != nil {
					b.Fatal(err)
				}

				if !drain.ValidMAC(mac) {
					b.Errorf("Invalid MAC: %q", drain.MAC())
				}

				drain.Reset()
			}
		})
	})
}
