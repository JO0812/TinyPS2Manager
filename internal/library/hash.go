package library

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
)

// hashPrefixBytes bounds the content read for hashing: SHA-256 over the
// first 1 MiB plus the total size (plan §3.2 #1). Cheap, stable, and
// sufficient for dedupe and override-cache keying; a full re-hash on
// collision is left to the caller and has never been needed in practice.
const hashPrefixBytes = 1 << 20

// ContentHash keys a source file by content: hex(sha256(first 1 MiB)):size.
func ContentHash(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", 0, err
	}
	h := sha256.New()
	if _, err := io.CopyN(h, f, hashPrefixBytes); err != nil && err != io.EOF {
		return "", 0, err
	}
	return fmt.Sprintf("%x:%d", h.Sum(nil), fi.Size()), fi.Size(), nil
}
