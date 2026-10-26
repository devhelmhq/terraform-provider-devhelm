package bundle

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// vectorsFile is mono's docs/contracts/bundle-vectors.json, vendored by
// scripts/regen-from.sh next to the OpenAPI spec. The server writes it from
// its own digest implementation, so these tests prove the provider and the
// server agree on every digest (doc 42 §5.3).
const vectorsFile = "../../docs/contracts/bundle-vectors.json"

type vector struct {
	Name    string `json:"name"`
	Request struct {
		Entrypoint  *string           `json:"entrypoint"`
		Include     []string          `json:"include"`
		Files       map[string]string `json:"files"`
		BinaryFiles map[string]string `json:"binaryFiles"`
		Symlinks    map[string]string `json:"symlinks"`
	} `json:"request"`
	Expect struct {
		Digest       string   `json:"digest"`
		UploadDigest string   `json:"uploadDigest"`
		Files        []string `json:"files"`
		Refusal      *struct {
			Code string `json:"code"`
		} `json:"refusal"`
	} `json:"expect"`
}

func loadVectors(t *testing.T) []vector {
	t.Helper()
	raw, err := os.ReadFile(vectorsFile)
	if errors.Is(err, os.ErrNotExist) {
		t.Fatalf("%s is missing; run scripts/regen-from.sh against mono", vectorsFile)
	}
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Vectors []vector `json:"vectors"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Vectors) == 0 {
		t.Fatal("no vectors")
	}
	return doc.Vectors
}

// materialize writes a vector's request to dir and returns the file map
// as bytes.
func materialize(t *testing.T, dir string, v vector) map[string][]byte {
	t.Helper()
	files := map[string][]byte{}
	put := func(p string, b []byte) {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, b, 0o644); err != nil {
			t.Fatal(err)
		}
		files[NormalizePath(p)] = b
	}
	for p, c := range v.Request.Files {
		put(p, []byte(c))
	}
	for p, c := range v.Request.BinaryFiles {
		b, err := base64.StdEncoding.DecodeString(c)
		if err != nil {
			t.Fatal(err)
		}
		put(p, b)
	}
	for p, target := range v.Request.Symlinks {
		full := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(target, full); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	return files
}

func entryFor(v vector, files map[string][]byte) string {
	if v.Request.Entrypoint != nil {
		return *v.Request.Entrypoint
	}
	var specs []string
	for p := range files {
		if strings.HasSuffix(p, ".spec.ts") {
			specs = append(specs, p)
		}
	}
	sort.Strings(specs)
	return specs[0]
}

func TestBundleVectors_DigestsMatchTheServer(t *testing.T) {
	for _, v := range loadVectors(t) {
		t.Run(v.Name, func(t *testing.T) {
			dir := t.TempDir()
			files := materialize(t, dir, v)
			entry := entryFor(v, files)
			in := Input{Entrypoint: entry, Root: dir, WorkDir: dir, Include: v.Request.Include}
			// Files outside the entry directory (a root tsconfig.json, a
			// shared lib/) come in through `include`, as a user would
			// write them.
			for p := range files {
				if !strings.HasPrefix(p, filepath.ToSlash(filepath.Dir(entry))+"/") && len(v.Request.Include) == 0 {
					in.Include = []string{"**"}
				}
			}
			set, err := Build(in)

			if v.Expect.Refusal != nil {
				if v.Expect.Refusal.Code == "IMPORT_NOT_IN_BUNDLE" {
					// Include-set clients don't parse JS; the server refuses
					// this one at upload. The set itself builds.
					if err != nil {
						t.Fatalf("include set should build and leave the import check to the server: %v", err)
					}
					return
				}
				var be *Error
				if !errors.As(err, &be) || be.Code != v.Expect.Refusal.Code {
					t.Fatalf("err = %v, want refusal %s", err, v.Expect.Refusal.Code)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			// The provider sends the include set: everything in the request.
			if got, want := set.Paths(), sortedKeys(files); !reflect.DeepEqual(got, want) {
				t.Fatalf("include set = %v, want %v", got, want)
			}
			// The explicit include paths go to the server as `include`.
			if len(v.Request.Include) > 0 && !reflect.DeepEqual(set.Included, v.Request.Include) {
				t.Errorf("included = %v, want %v", set.Included, v.Request.Include)
			}
			if got := set.UploadDigest(); got != v.Expect.UploadDigest {
				t.Errorf("uploadDigest = %s, want %s", got, v.Expect.UploadDigest)
			}
			closure := map[string][]byte{}
			for _, p := range v.Expect.Files {
				closure[p] = set.Files[p]
			}
			if got := Digest(closure); got != v.Expect.Digest {
				t.Errorf("closure digest = %s, want %s", got, v.Expect.Digest)
			}
		})
	}
}

func sortedKeys(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
