// Package bundle builds the include set the provider uploads for a code
// monitor (doc 42 §5.3, doc 32 D-12) and computes the D-12 bundle digest.
//
// The provider is an include-set client: it never parses JavaScript. It
// sends the entry file's directory (or the `include` globs), minus the
// default excludes and `.devhelmignore`, and the server resolves the import
// closure. The closure digest the server returns is the revision identity;
// the digest computed here over the uploaded set must equal the server's
// `uploadDigest`, which proves the server hashed the bytes this provider
// read from disk.
package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"path"
	"sort"
	"strings"
)

// textExtensions are the extensions whose CRLF line endings become LF before
// hashing (D-12). Every other file is hashed byte for byte.
var textExtensions = map[string]struct{}{
	".ts": {}, ".tsx": {}, ".js": {}, ".jsx": {}, ".mjs": {}, ".cjs": {}, ".json": {},
	".yml": {}, ".yaml": {}, ".md": {}, ".txt": {}, ".html": {}, ".css": {},
}

// Digest returns the D-12 digest of a file map: `sha256:` plus lowercase hex
// of SHA-256 over, for each file in ascending UTF-8 byte order of its path,
// `u32be(len(path)) ‖ path ‖ u64be(len(content)) ‖ content`. Paths must
// already be normalized (see NormalizePath).
func Digest(files map[string][]byte) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	// Go string comparison is byte-wise, which is the UTF-8 byte order D-12
	// requires.
	sort.Strings(paths)

	h := sha256.New()
	var u32 [4]byte
	var u64 [8]byte
	for _, p := range paths {
		content := normalizeContent(p, files[p])
		binary.BigEndian.PutUint32(u32[:], uint32(len(p)))
		h.Write(u32[:])
		h.Write([]byte(p))
		binary.BigEndian.PutUint64(u64[:], uint64(len(content)))
		h.Write(u64[:])
		h.Write(content)
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

// FileDigest returns the per-file digest the provider records in
// `code_files`: `sha256:` plus hex of SHA-256 over the D-12-normalized
// content, so a CRLF-only edit to a text file is not a change.
func FileDigest(p string, content []byte) string {
	sum := sha256.Sum256(normalizeContent(p, content))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeContent(p string, content []byte) []byte {
	if !isTextPath(p) {
		return content
	}
	if !bytes.Contains(content, []byte("\r\n")) {
		return content
	}
	return bytes.ReplaceAll(content, []byte("\r\n"), []byte("\n"))
}

func isTextPath(p string) bool {
	ext := strings.ToLower(path.Ext(path.Base(p)))
	_, ok := textExtensions[ext]
	return ok
}
