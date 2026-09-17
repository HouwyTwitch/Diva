package annexb

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestReadAccessUnit(t *testing.T) {
	// Mix three- and four-byte Annex-B delimiters. Each access unit starts with AUD.
	stream := []byte{0, 0, 1, 0x09, 0xf0, 0, 0, 0, 1, 0x65, 1, 2, 0, 0, 1, 0x09, 0xf0, 0, 0, 1, 0x41, 3}
	r := New(bytes.NewReader(stream))
	first, err := r.ReadAccessUnit()
	if err != nil {
		t.Fatalf("first frame: %v", err)
	}
	want := []byte{0, 0, 0, 1, 0x09, 0xf0, 0, 0, 0, 1, 0x65, 1, 2}
	if !bytes.Equal(first, want) {
		t.Fatalf("first frame = %v, want %v", first, want)
	}
	second, err := r.ReadAccessUnit()
	if !errors.Is(err, io.EOF) {
		t.Fatalf("last frame error = %v, want EOF", err)
	}
	if len(second) == 0 || second[4]&31 != 9 {
		t.Fatalf("last frame does not begin with AUD: %v", second)
	}
}
