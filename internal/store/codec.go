package store

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
)

// compress encodes the stored document so a store of many large specs does not
// bloat the project checkout. gzip is enough: the blob is only ever read back
// whole by this package.
func compress(raw []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("gzip writer: %w", err)
	}
	if _, err := zw.Write(raw); err != nil {
		return nil, fmt.Errorf("compress document: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("compress document: %w", err)
	}
	return buf.Bytes(), nil
}

// decompress reverses compress. It is also what makes the stored blob portable
// across gzip implementations.
func decompress(blob []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(blob))
	if err != nil {
		return nil, fmt.Errorf("read stored document: %w", err)
	}
	defer zr.Close()
	raw, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("read stored document: %w", err)
	}
	return raw, nil
}
