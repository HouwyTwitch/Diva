// Package annexb splits an H.264 Annex-B byte stream into access units.
package annexb

import (
	"bufio"
	"io"
)

// Reader uses AUD NAL units (type 9) as frame boundaries. Diva asks FFmpeg to
// insert an AUD before every encoded frame.
type Reader struct {
	r                 *bufio.Reader
	delimiterConsumed bool
	pendingNAL        []byte
}

func New(r io.Reader) *Reader { return &Reader{r: bufio.NewReaderSize(r, 1<<20)} }

func (h *Reader) ReadAccessUnit() ([]byte, error) {
	var out []byte
	for {
		nal, err := h.readNAL()
		if len(nal) > 4 && (nal[4]&31) == 9 && len(out) > 0 {
			h.pendingNAL = nal
			return out, nil
		}
		out = append(out, nal...)
		if err != nil {
			return out, err
		}
	}
}

func (h *Reader) readNAL() ([]byte, error) {
	if len(h.pendingNAL) > 0 {
		n := h.pendingNAL
		h.pendingNAL = nil
		return n, nil
	}
	prefix := []byte{0, 0, 0, 1}
	var out []byte
	if h.delimiterConsumed {
		out = append(out, prefix...)
		h.delimiterConsumed = false
	}
	zeros := 0
	for {
		b, err := h.r.ReadByte()
		if err != nil {
			return out, err
		}
		if b == 0 {
			zeros++
			continue
		}
		if b == 1 && zeros >= 2 {
			if len(out) > 0 {
				h.delimiterConsumed = true
				return out, nil
			}
			out = append(out, prefix...)
			zeros = 0
			continue
		}
		out = append(out, make([]byte, zeros)...)
		zeros = 0
		out = append(out, b)
	}
}
