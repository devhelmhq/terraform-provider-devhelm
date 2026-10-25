package bundle

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// frame is an independent implementation of the D-12 framing for one file,
// mirroring the server's BundleDigestCalculatorTest vector.
func frame(path string, content []byte) []byte {
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(len(path)))
	b.WriteString(path)
	_ = binary.Write(&b, binary.BigEndian, uint64(len(content)))
	b.Write(content)
	return b.Bytes()
}

func sha(b []byte) string {
	s := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(s[:])
}

func TestDigest_FramingMatchesLengthPrefixedVector(t *testing.T) {
	got := Digest(map[string][]byte{"a.txt": []byte("hi\n")})
	if want := sha(frame("a.txt", []byte("hi\n"))); got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
}

func TestDigest_SortsByUTF8BytesRegardlessOfInsertionOrder(t *testing.T) {
	files := map[string][]byte{"b.ts": []byte("export const b = 1;\n"), "a.ts": []byte("export const a = 1;\n"), "é.ts": []byte("x")}
	want := sha(append(append(frame("a.ts", files["a.ts"]), frame("b.ts", files["b.ts"])...), frame("é.ts", files["é.ts"])...))
	if got := Digest(files); got != want {
		t.Fatalf("digest = %s, want %s", got, want)
	}
}

func TestDigest_CRLFTextMatchesLFButBinaryDoesNot(t *testing.T) {
	lf, crlf := []byte("line\n"), []byte("line\r\n")
	if Digest(map[string][]byte{"a.ts": crlf}) != Digest(map[string][]byte{"a.ts": lf}) {
		t.Error("CRLF .ts must hash like LF")
	}
	if Digest(map[string][]byte{"A.JSON": crlf}) != Digest(map[string][]byte{"A.JSON": lf}) {
		t.Error("extension match is case-insensitive")
	}
	if Digest(map[string][]byte{"a.png": crlf}) == Digest(map[string][]byte{"a.png": lf}) {
		t.Error("binary files hash byte for byte")
	}
	if FileDigest("a.ts", crlf) != FileDigest("a.ts", lf) {
		t.Error("per-file digest ignores CRLF on text files")
	}
}

func TestNormalizePath_NFCAndPOSIX(t *testing.T) {
	decomposed := "café/x.ts" // e + combining acute
	if got := NormalizePath("./" + decomposed); got != "café/x.ts" {
		t.Errorf("NormalizePath = %q, want NFC", got)
	}
	if got := NormalizePath(`a\b.ts`); got != "a/b.ts" {
		t.Errorf("NormalizePath = %q, want POSIX separators", got)
	}
}

// write creates files under dir; content keyed by slash path.
func write(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for p, c := range files {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(c), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func keys(s *Set) []string { return s.Paths() }

func TestBuild_DefaultIncludeSetIsEntryDirMinusExcludes(t *testing.T) {
	repo := t.TempDir()
	write(t, repo, map[string]string{
		".git/HEAD":                          "ref",
		"synthetics/checkout.spec.ts":        "import './lib/cart'",
		"synthetics/lib/cart.ts":             "export {}",
		"synthetics/auth.setup.ts":           "setup",
		"synthetics/data.json":               "{}",
		"synthetics/node_modules/x/index.js": "x",
		"synthetics/dist/out.js":             "x",
		"synthetics/build/out.js":            "x",
		"synthetics/coverage/lcov.info":      "x",
		"synthetics/lib/cart.ts.map":         "x",
		"synthetics/.env":                    "SECRET=1",
		"synthetics/.env.local":              "SECRET=1",
		"synthetics/.eslintrc":               "{}",
		"other/unrelated.ts":                 "x",
	})
	s, err := Build(Input{Entrypoint: filepath.Join(repo, "synthetics/checkout.spec.ts")})
	if err != nil {
		t.Fatal(err)
	}
	if s.Root != repo {
		t.Errorf("root = %s, want the .git ancestor %s", s.Root, repo)
	}
	if s.Entrypoint != "synthetics/checkout.spec.ts" {
		t.Errorf("entrypoint = %s", s.Entrypoint)
	}
	want := []string{"synthetics/auth.setup.ts", "synthetics/checkout.spec.ts", "synthetics/data.json", "synthetics/lib/cart.ts"}
	if got := keys(s); !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
	if s.UploadDigest() != Digest(s.Files) {
		t.Error("UploadDigest must be the D-12 digest of the set")
	}
}

func TestBuild_RootFallsBackToEntryDirWithoutGit(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"checks/a.spec.ts": "x"})
	s, err := Build(Input{Entrypoint: "checks/a.spec.ts", WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	// TempDir has no .git ancestor on CI runners; tolerate a developer
	// machine whose temp dir happens to sit under a repo.
	if findGitRoot(filepath.Join(dir, "checks")) == "" && s.Entrypoint != "a.spec.ts" {
		t.Errorf("entrypoint = %s, want a.spec.ts relative to the entry dir", s.Entrypoint)
	}
}

func TestBuild_IncludeGlobsNarrowAndExplicitRoot(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		"synthetics/checkout.spec.ts": "x",
		"synthetics/helpers/a.ts":     "x",
		"synthetics/fixtures/big.txt": "x",
		"shared/util.ts":              "x",
	})
	s, err := Build(Input{
		Entrypoint: "synthetics/checkout.spec.ts",
		Include:    []string{"synthetics/helpers/**", "shared/*.ts"},
		Root:       ".",
		WorkDir:    dir,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"shared/util.ts", "synthetics/checkout.spec.ts", "synthetics/helpers/a.ts"}
	if got := keys(s); !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestBuild_DevhelmignoreIsHonoured(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{
		".devhelmignore":        "fixtures/\n*.snap\n!keep.snap\n# comment\n",
		"a.spec.ts":             "x",
		"fixtures/big.json":     "x",
		"x.snap":                "x",
		"keep.snap":             "x",
		"nested/fixtures/f.txt": "x",
	})
	s, err := Build(Input{Entrypoint: "a.spec.ts", Root: ".", WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.spec.ts", "keep.snap"}
	if got := keys(s); !reflect.DeepEqual(got, want) {
		t.Errorf("files = %v, want %v", got, want)
	}
}

func TestBuild_SymlinkIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"a.spec.ts": "x", "real.ts": "x"})
	if err := os.Symlink(filepath.Join(dir, "real.ts"), filepath.Join(dir, "link.ts")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, err := Build(Input{Entrypoint: "a.spec.ts", Root: ".", WorkDir: dir})
	var be *Error
	if !errors.As(err, &be) || be.Code != CodePathInvalid {
		t.Fatalf("err = %v, want %s symlink refusal", err, CodePathInvalid)
	}
}

func TestBuild_EntrypointErrors(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"sub/a.spec.ts": "x", ".hidden/b.spec.ts": "x"})
	cases := map[string]Input{
		"missing":      {Entrypoint: "nope.ts", WorkDir: dir},
		"outside root": {Entrypoint: "sub/a.spec.ts", Root: "sub/other", WorkDir: dir},
		"dotfile":      {Entrypoint: ".hidden/b.spec.ts", Root: ".", WorkDir: dir},
		"blank":        {Entrypoint: " ", WorkDir: dir},
	}
	for name, in := range cases {
		if _, err := Build(in); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestBuild_IncludeOutsideRootIsRefused(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"a/a.spec.ts": "x"})
	_, err := Build(Input{Entrypoint: "a/a.spec.ts", Root: "a", Include: []string{"../elsewhere/**"}, WorkDir: dir})
	if err == nil {
		t.Fatal("expected include outside root to be refused")
	}
}

func TestBuild_LimitsAreEnforcedWithLargestDirectories(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{"a.spec.ts": "x"}
	for i := 0; i < MaxFiles+1; i++ {
		files[filepath.ToSlash(filepath.Join("many", string(rune('a'+i%26)), "f"+string(rune('a'+i/26))+".ts"))] = "x"
	}
	write(t, dir, files)
	_, err := Build(Input{Entrypoint: "a.spec.ts", Root: ".", WorkDir: dir})
	var be *Error
	if !errors.As(err, &be) || be.Code != CodeTooLarge {
		t.Fatalf("err = %v, want %s", err, CodeTooLarge)
	}
	if !bytes.Contains([]byte(be.Message), []byte("many (")) {
		t.Errorf("message should list the largest directories: %s", be.Message)
	}
}

func TestBuild_BinaryFileIsKeptByteForByte(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, map[string]string{"a.spec.ts": "x"})
	bin := []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0xff}
	if err := os.WriteFile(filepath.Join(dir, "logo.png"), bin, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Build(Input{Entrypoint: "a.spec.ts", Root: ".", WorkDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(s.Files["logo.png"], bin) {
		t.Errorf("binary content changed")
	}
}
