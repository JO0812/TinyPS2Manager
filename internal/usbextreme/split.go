package usbextreme

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/jo/TinyPS2Manager/internal/cuebin"
)

// Wire format (verified against the OPL wiki usb-mode page and the
// MIT-licensed ulmake implementation — reimplemented here in Go, no code
// shared):
//
//   - Chunk size is exactly 1 GiB (1,073,741,824 bytes).
//   - Chunks are named ul.<CRC>.<SERIAL>.0<i>, e.g.
//     ul.84BA9D95.SLXS_123.45.00, and live at the device root.
//   - <CRC> is a custom MSB-first CRC (poly 0x04C11DB7, init 0, no
//     reflection) over the OPL name bytes plus a trailing NUL, rendered as
//     unpadded uppercase hex. It is NOT standard CRC32.
//   - ul.cfg at the device root holds one 64-byte record per game:
//     [0:32] OPL name NUL-padded, [32:35] "ul.", [35:47] serial NUL-padded
//     to 12, [47] chunk count, [48] 0x14 media type, [49:53] zeros,
//     [53] 0x08 magic, [54:64] zeros.
//
// Serial identity is the 11-char SXXX_NNN.NN form (cuebin.ValidSerial);
// chunk names and the record's serial field carry it verbatim.
const (
	// ChunkSize is the published USBExtreme chunk size: 1 GiB.
	ChunkSize int64 = 1 << 30

	recordSize      = 64
	nameFieldSize   = 32
	serialFieldSize = 12
	mediaTypeDVD    = 0x14
	formatMagic     = 0x08

	copyBufferSize = 1 << 20
	maxChunks      = 255 // chunk count field is one byte
)

var crcTable = buildCRCTable()

func buildCRCTable() [256]uint32 {
	var t [256]uint32
	for i := 0; i < 256; i++ {
		crc := uint32(i) << 24
		for k := 0; k < 8; k++ {
			// NOTE: branches are intentionally "inverted" vs textbook
			// MSB-first CRC: top-bit-set shifts WITHOUT the polynomial.
			// This replicates the reference implementation's i32
			// sign-bit logic exactly; "fixing" it breaks chunk-name
			// compatibility (see TestGameCRC vectors).
			if crc&0x80000000 != 0 {
				crc <<= 1
			} else {
				crc = (crc << 1) ^ 0x04C11DB7
			}
		}
		t[255-i] = crc
	}
	return t
}

// GameCRC returns the USBExtreme name hash for an OPL title: the custom
// MSB-first CRC over the name bytes plus a trailing NUL, as unpadded
// uppercase hex.
func GameCRC(oplName string) string {
	data := append([]byte(oplName), 0x00)
	var crc uint32
	for _, b := range data {
		idx := b ^ byte(crc>>24)
		crc = crcTable[idx] ^ (crc << 8)
	}
	return fmt.Sprintf("%X", crc)
}

// ChunkName returns the on-device chunk filename for the index-th chunk.
func ChunkName(oplName, serial string, index int) string {
	return fmt.Sprintf("ul.%s.%s.0%d", GameCRC(oplName), serial, index)
}

// Entry is one game's ul.cfg record.
type Entry struct {
	OPLName string
	Serial  string
	Chunks  int
}

func cfgPath(dir string) string { return filepath.Join(dir, "ul.cfg") }

// Write splits src (exactly size bytes) into dir and upserts the game's
// ul.cfg record. dir is the device root (spec §12 correction: ul.cfg +
// ul.* live at root, not in DVD/). Chunks from a previous set under the
// same serial are removed. On failure, created chunks are deleted and
// ul.cfg is left untouched (chunks complete before the record updates).
func Write(dir, oplName, serial string, src io.Reader, size int64) (int, error) {
	return writeWithChunkSize(dir, oplName, serial, src, size, ChunkSize)
}

func writeWithChunkSize(dir, oplName, serial string, src io.Reader, size, chunkSize int64) (int, error) {
	if len(oplName) == 0 || len(oplName) > nameFieldSize {
		return 0, fmt.Errorf("OPL name must be 1-32 bytes, got %d", len(oplName))
	}
	if !cuebin.ValidSerial(serial) {
		return 0, fmt.Errorf("bad serial %q", serial)
	}
	if size <= 0 {
		return 0, fmt.Errorf("size must be positive, got %d", size)
	}
	if chunkSize <= 0 {
		return 0, fmt.Errorf("chunk size must be positive, got %d", chunkSize)
	}
	n := int((size + chunkSize - 1) / chunkSize)
	if n > maxChunks {
		return 0, fmt.Errorf("%d chunks exceeds USBExtreme count limit %d", n, maxChunks)
	}

	old, err := findRecord(dir, serial)
	if err != nil {
		return 0, err // fail closed on a corrupt index
	}

	crc := GameCRC(oplName)
	names := make([]string, n)
	for i := range names {
		names[i] = fmt.Sprintf("ul.%s.%s.0%d", crc, serial, i)
	}

	buf := make([]byte, copyBufferSize)
	var created []string
	cleanup := func() {
		for _, name := range created {
			os.Remove(filepath.Join(dir, name))
		}
	}
	remaining := size
	for _, name := range names {
		want := min64(remaining, chunkSize)
		if err := writeChunk(filepath.Join(dir, name), src, want, buf); err != nil {
			cleanup()
			return 0, err
		}
		created = append(created, name)
		remaining -= want
	}
	// The source must be exactly size bytes: one more byte is an error.
	if b := make([]byte, 1); readFull(src, b) {
		cleanup()
		return 0, fmt.Errorf("source larger than stated size %d", size)
	}

	removeStaleChunks(dir, old, names)
	if err := upsertRecord(dir, Entry{OPLName: oplName, Serial: serial, Chunks: n}); err != nil {
		cleanup()
		return 0, err
	}
	return n, nil
}

// writeChunk streams exactly want bytes from src to path.
func writeChunk(path string, src io.Reader, want int64, buf []byte) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	n, copyErr := io.CopyBuffer(f, io.LimitReader(src, want), buf)
	closeErr := f.Close()
	if copyErr != nil {
		return fmt.Errorf("write %s: %w", filepath.Base(path), copyErr)
	}
	if n != want {
		return fmt.Errorf("source smaller than stated size: got %d of %d chunk bytes", n, want)
	}
	return closeErr
}

// readFull reports whether one full byte could be read.
func readFull(r io.Reader, b []byte) bool {
	_, err := io.ReadFull(r, b)
	return err == nil
}

// removeStaleChunks deletes the previous set's chunks not in the new set.
func removeStaleChunks(dir string, old *Entry, names []string) {
	if old == nil {
		return
	}
	keep := map[string]bool{}
	for _, n := range names {
		keep[n] = true
	}
	oldCRC := GameCRC(old.OPLName)
	for i := 0; i < old.Chunks; i++ {
		name := fmt.Sprintf("ul.%s.%s.0%d", oldCRC, old.Serial, i)
		if !keep[name] {
			os.Remove(filepath.Join(dir, name))
		}
	}
}

// List parses dir/ul.cfg. A missing file means no games (empty, nil).
func List(dir string) ([]Entry, error) {
	raw, err := os.ReadFile(cfgPath(dir))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(raw)%recordSize != 0 {
		return nil, fmt.Errorf("ul.cfg size %d is not a multiple of %d (corrupt)", len(raw), recordSize)
	}
	var out []Entry
	for off := 0; off < len(raw); off += recordSize {
		e, err := decodeRecord(raw[off : off+recordSize])
		if err != nil {
			return nil, fmt.Errorf("ul.cfg record %d: %w", off/recordSize, err)
		}
		out = append(out, e)
	}
	return out, nil
}

// findRecord returns the entry for serial, or nil when absent.
func findRecord(dir, serial string) (*Entry, error) {
	entries, err := List(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if e.Serial == serial {
			c := e
			return &c, nil
		}
	}
	return nil, nil
}

// upsertRecord replaces the same-serial entry or appends, then rewrites
// ul.cfg whole (the file is tiny: 64 bytes per game).
func upsertRecord(dir string, e Entry) error {
	entries, err := List(dir)
	if err != nil {
		return err
	}
	rec, err := encodeRecord(e) // validates before touching the file
	if err != nil {
		return err
	}
	var raw []byte
	replaced := false
	for _, cur := range entries {
		if cur.Serial == e.Serial {
			cur = e
			replaced = true
		}
		r, err := encodeRecord(cur)
		if err != nil {
			return err
		}
		raw = append(raw, r...)
	}
	if !replaced {
		raw = append(raw, rec...)
	}
	return os.WriteFile(cfgPath(dir), raw, 0o644)
}

func encodeRecord(e Entry) ([]byte, error) {
	if len(e.OPLName) == 0 || len(e.OPLName) > nameFieldSize {
		return nil, fmt.Errorf("OPL name must be 1-32 bytes, got %d", len(e.OPLName))
	}
	if !cuebin.ValidSerial(e.Serial) {
		return nil, fmt.Errorf("bad serial %q", e.Serial)
	}
	if e.Chunks < 1 || e.Chunks > maxChunks {
		return nil, fmt.Errorf("chunk count %d out of range 1-%d", e.Chunks, maxChunks)
	}
	rec := make([]byte, recordSize)
	copy(rec[0:nameFieldSize], e.OPLName)
	copy(rec[nameFieldSize:nameFieldSize+3], "ul.")
	copy(rec[nameFieldSize+3:nameFieldSize+3+serialFieldSize], e.Serial)
	rec[nameFieldSize+3+serialFieldSize] = byte(e.Chunks) // offset 47
	rec[48] = mediaTypeDVD
	rec[53] = formatMagic
	return rec, nil
}

func decodeRecord(rec []byte) (Entry, error) {
	if len(rec) != recordSize {
		return Entry{}, fmt.Errorf("record size %d, want %d", len(rec), recordSize)
	}
	name := stripNULs(rec[0:nameFieldSize])
	if len(name) == 0 {
		return Entry{}, fmt.Errorf("empty game name")
	}
	if string(rec[nameFieldSize:nameFieldSize+3]) != "ul." {
		return Entry{}, fmt.Errorf("missing ul. prefix")
	}
	serial := stripNULs(rec[nameFieldSize+3 : nameFieldSize+3+serialFieldSize])
	if !cuebin.ValidSerial(serial) {
		return Entry{}, fmt.Errorf("bad serial %q", serial)
	}
	chunks := int(rec[nameFieldSize+3+serialFieldSize])
	if chunks < 1 {
		return Entry{}, fmt.Errorf("chunk count %d", chunks)
	}
	// Magic bytes (0x14 at 48, 0x08 at 53) are deliberately not enforced:
	// other compliant tools may vary them, and OPL keys off name/serial/count.
	return Entry{OPLName: name, Serial: serial, Chunks: chunks}, nil
}

// stripNULs removes all NUL bytes, matching how other tools read the
// NUL-padded fields.
func stripNULs(b []byte) string {
	return strings.ReplaceAll(string(b), "\x00", "")
}

// multiCloser chains chunk files behind one reader.
type multiCloser struct {
	io.Reader
	files []*os.File
}

func (m *multiCloser) Close() error {
	var first error
	for _, f := range m.files {
		if err := f.Close(); err != nil && first == nil {
			first = err
		}
	}
	return first
}

// Open returns a reader over the game's chunks in index order, plus the
// total byte size. Every chunk's presence and exact size is verified first
// (fail closed); content integrity is the caller's hash pass.
func Open(dir, serial string) (io.ReadCloser, int64, error) {
	return openWithChunkSize(dir, serial, ChunkSize)
}

func openWithChunkSize(dir, serial string, chunkSize int64) (io.ReadCloser, int64, error) {
	rec, err := findRecord(dir, serial)
	if err != nil {
		return nil, 0, err
	}
	if rec == nil {
		return nil, 0, fmt.Errorf("no ul.cfg entry for %s", serial)
	}
	crc := GameCRC(rec.OPLName)
	var readers []io.Reader
	var files []*os.File
	var total int64
	closeFiles := func() {
		for _, f := range files {
			f.Close()
		}
	}
	for i := 0; i < rec.Chunks; i++ {
		name := fmt.Sprintf("ul.%s.%s.0%d", crc, rec.Serial, i)
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			closeFiles()
			return nil, 0, fmt.Errorf("chunk %s: %w", name, err)
		}
		want := chunkSize
		if i == rec.Chunks-1 {
			if fi.Size() < 1 || fi.Size() > chunkSize {
				closeFiles()
				return nil, 0, fmt.Errorf("chunk %s size %d out of range", name, fi.Size())
			}
		} else if fi.Size() != want {
			closeFiles()
			return nil, 0, fmt.Errorf("chunk %s size %d, want %d", name, fi.Size(), want)
		}
		f, err := os.Open(filepath.Join(dir, name))
		if err != nil {
			closeFiles()
			return nil, 0, fmt.Errorf("chunk %s: %w", name, err)
		}
		files = append(files, f)
		readers = append(readers, f)
		total += fi.Size()
	}
	return &multiCloser{Reader: io.MultiReader(readers...), files: files}, total, nil
}

// Verify checks the record and every chunk's presence and size without
// reading content: the fast pre-flight (§2.11) before any hash pass.
func Verify(dir, serial string) error {
	return verifyWithChunkSize(dir, serial, ChunkSize)
}

func verifyWithChunkSize(dir, serial string, chunkSize int64) error {
	r, total, err := openWithChunkSize(dir, serial, chunkSize)
	if err != nil {
		return err
	}
	r.Close()
	if total < 1 {
		return fmt.Errorf("game %s has no data", serial)
	}
	return nil
}

func min64(a, b int64) int64 {
	if a < b {
		return a
	}
	return b
}
