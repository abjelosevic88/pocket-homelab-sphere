package server

import (
	"encoding/binary"
	"io"
	"os"
	"sync"
	"time"
)

// HEVC in MP4 comes as hvc1 (the decoder's set-up only in the header) or hev1
// (it may repeat in the stream). Apple's players refuse hev1 — sound, black
// picture — though such files nearly always carry the full set-up in the header
// too. Served as they are, these files are relabelled hvc1 on the way out: four
// bytes in the sample description, nothing re-encoded, seeking as usual. Only
// when the header has the parameter sets (VPS, SPS, PPS), so the relabel is
// sound; downloads get the file untouched.

type hev1Key struct {
	path  string
	size  int64
	mtime time.Time
}

var hev1Cache sync.Map // hev1Key → []int64

// hev1Offsets finds the "hev1" sample entry types to rewrite (cached per file version).
func hev1Offsets(f *os.File, fi os.FileInfo) []int64 {
	key := hev1Key{f.Name(), fi.Size(), fi.ModTime()}
	if v, ok := hev1Cache.Load(key); ok {
		return v.([]int64)
	}
	var out []int64
	walkMP4(f, 0, fi.Size(), 0, &out)
	hev1Cache.Store(key, out)
	return out
}

func readAt(f io.ReaderAt, off int64, n int) []byte {
	b := make([]byte, n)
	if _, err := f.ReadAt(b, off); err != nil {
		return nil
	}
	return b
}

// boxes calls fn for each box in [start, end): its offset, size and header length.
func mp4Boxes(f io.ReaderAt, start, end int64, fn func(pos, size, hlen int64, typ string)) {
	for pos := start; pos+8 <= end; {
		h := readAt(f, pos, 8)
		if h == nil {
			return
		}
		size, hlen := int64(binary.BigEndian.Uint32(h)), int64(8)
		switch size {
		case 1:
			b := readAt(f, pos+8, 8)
			if b == nil {
				return
			}
			size, hlen = int64(binary.BigEndian.Uint64(b)), 16
		case 0:
			size = end - pos
		}
		if size < hlen || pos+size > end {
			return
		}
		fn(pos, size, hlen, string(h[4:8]))
		pos += size
	}
}

func walkMP4(f io.ReaderAt, start, end int64, depth int, out *[]int64) {
	if depth > 6 {
		return
	}
	mp4Boxes(f, start, end, func(pos, size, hlen int64, typ string) {
		switch typ {
		case "moov", "trak", "mdia", "minf", "stbl":
			walkMP4(f, pos+hlen, pos+size, depth+1, out)
		case "stsd": // full box header (4) and entry count (4), then the entries
			mp4Boxes(f, pos+hlen+8, pos+size, func(epos, esize, ehlen int64, etyp string) {
				if etyp == "hev1" && hvcCHasParameterSets(f, epos+ehlen+78, epos+esize) {
					*out = append(*out, epos+4)
				}
			})
		}
	})
}

// hvcCHasParameterSets: the visual sample entry's hvcC lists a VPS, an SPS and a PPS.
func hvcCHasParameterSets(f io.ReaderAt, start, end int64) bool {
	ok := false
	mp4Boxes(f, start, end, func(pos, size, hlen int64, typ string) {
		if typ != "hvcC" || ok {
			return
		}
		b := readAt(f, pos+hlen, int(min(size-hlen, 64<<10)))
		if len(b) < 23 {
			return
		}
		n, i := int(b[22]), 23
		seen := map[byte]bool{}
		for a := 0; a < n && i+3 <= len(b); a++ {
			t, cnt := b[i]&0x3f, int(binary.BigEndian.Uint16(b[i+1:]))
			i += 3
			for k := 0; k < cnt && i+2 <= len(b); k++ {
				i += 2 + int(binary.BigEndian.Uint16(b[i:]))
			}
			if cnt > 0 {
				seen[t] = true
			}
		}
		ok = seen[32] && seen[33] && seen[34]
	})
	return ok
}

// relabelled reads a file with "hvc1" written over the given offsets.
type relabelled struct {
	f       *os.File
	pos     int64
	patches []int64
}

func (r *relabelled) Read(p []byte) (int, error) {
	n, err := r.f.Read(p)
	for _, off := range r.patches {
		for k := int64(0); k < 4; k++ {
			if i := off + k - r.pos; i >= 0 && i < int64(n) {
				p[i] = "hvc1"[k]
			}
		}
	}
	r.pos += int64(n)
	return n, err
}

func (r *relabelled) Seek(offset int64, whence int) (int64, error) {
	pos, err := r.f.Seek(offset, whence)
	if err == nil {
		r.pos = pos
	}
	return pos, err
}
