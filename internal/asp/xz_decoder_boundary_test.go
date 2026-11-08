package asp

import (
	"bytes"
	"io"
	"testing"

	"github.com/ulikunitz/xz"
)

func decodeXZFixture(blob []byte) error {
	reader, err := xz.NewReader(bytes.NewReader(blob))
	if err != nil {
		return err
	}
	_, err = io.Copy(io.Discard, reader)
	return err
}

func TestPayloadXZDecoderAcceptsValidBlocks(t *testing.T) {
	var compressed bytes.Buffer
	writer, err := (xz.WriterConfig{DictCap: 1 << 20, BlockSize: 4096}).NewWriter(&compressed)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write(bytes.Repeat([]byte("A"), 8192)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = decodeXZFixture(compressed.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func TestPayloadXZDecoderMalformedInputIsError(t *testing.T) {
	for _, sample := range [][]byte{nil, {0}, make([]byte, 24), bytes.Repeat([]byte{1}, 12)} {
		if err := decodeXZFixture(sample); err == nil {
			t.Fatal("accepted malformed XZ stream")
		}
	}
}
