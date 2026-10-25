package bundle

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/bmatcuk/doublestar/v4"
	"golang.org/x/text/unicode/norm"
)

// Limits shared with the server (doc 42 §3.1): 256 files, 5 MB uncompressed.
const (
	MaxFiles = 256
	MaxBytes = 5 * 1024 * 1024
)

// Error codes for client-side refusals. They match the server's codes for
// the same refusal, so a diagnostic reads the same whichever side caught it.
const (
	CodeTooLarge    = "BUNDLE_TOO_LARGE"
	CodePathInvalid = "MONITOR_PACKAGE_PATH_INVALID"
)

// DefaultSetupFile is the session setup file that is always included when it
// sits next to the entrypoint (doc 32 D-12).
const DefaultSetupFile = "auth.setup.ts"

// IgnoreFile is the gitignore-syntax file honoured at the bundle root and in
// the entry directory.
const IgnoreFile = ".devhelmignore"

// defaultExcludes is the doc 42 §5.3 exclusion list, in gitignore syntax.
// Dotfiles (`.git/`, `.env*`, `.devhelmignore`, …) are never uploaded at all:
// the server refuses any path segment that starts with a dot.
var defaultExcludes = []string{"node_modules/", "dist/", "build/", "coverage/", "*.map"}

// Input is the `code` block of a devhelm_monitor.
type Input struct {
	// Entrypoint is the file that runs (absolute, or relative to WorkDir).
	Entrypoint string
	// Include narrows the default include set to these globs (doublestar
	// syntax; absolute, or relative to WorkDir). Empty means the entry
	// file's directory, recursively.
	Include []string
	// Root is the bundle root. Empty means the nearest ancestor of the
	// entrypoint that contains `.git`, else the entrypoint's directory.
	Root string
	// WorkDir resolves relative paths. Empty means the process working
	// directory (the Terraform root module).
	WorkDir string
}

// Set is the resolved include set: the bundle root, the entrypoint relative
// to it, and every file to upload keyed by its normalized bundle path.
type Set struct {
	Root       string
	Entrypoint string
	Files      map[string][]byte
}

// Error is a client-side refusal with a stable code.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Code + ": " + e.Message }

func refuse(code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// UploadDigest is the D-12 digest of everything in the set; it must equal
// the server's `uploadDigest` for the same upload.
func (s *Set) UploadDigest() string { return Digest(s.Files) }

// Paths returns the set's bundle paths in sorted order.
func (s *Set) Paths() []string {
	out := make([]string, 0, len(s.Files))
	for p := range s.Files {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Build resolves the include set for in.
func Build(in Input) (*Set, error) {
	workDir := in.WorkDir
	if workDir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, err
		}
		workDir = wd
	}
	abs := func(p string) string {
		if filepath.IsAbs(p) {
			return filepath.Clean(p)
		}
		return filepath.Join(workDir, p)
	}

	if strings.TrimSpace(in.Entrypoint) == "" {
		return nil, refuse(CodePathInvalid, "code.entrypoint is required")
	}
	entry := abs(in.Entrypoint)
	info, err := os.Lstat(entry)
	if err != nil {
		return nil, refuse(CodePathInvalid, "entrypoint %s: %v", in.Entrypoint, unwrapPathError(err))
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return nil, refuse(CodePathInvalid, "entrypoint %s is a symlink; bundles never follow symlinks", in.Entrypoint)
	}
	if !info.Mode().IsRegular() {
		return nil, refuse(CodePathInvalid, "entrypoint %s is not a regular file", in.Entrypoint)
	}
	entryDir := filepath.Dir(entry)

	root := ""
	if in.Root != "" {
		root = abs(in.Root)
	} else {
		root = findGitRoot(entryDir)
		if root == "" {
			root = entryDir
		}
	}
	relEntry, err := bundlePath(root, entry)
	if err != nil {
		return nil, refuse(CodePathInvalid, "entrypoint %s is outside the bundle root %s", in.Entrypoint, root)
	}
	if hasDotSegment(relEntry) {
		return nil, refuse(CodePathInvalid, "entrypoint %s is a dotfile or under a dot directory; bundles cannot contain dotfiles", relEntry)
	}

	ignore := newIgnoreRules(defaultExcludes, "")
	for _, dir := range uniqueDirs(root, entryDir) {
		if err := ignore.loadFile(root, dir); err != nil {
			return nil, err
		}
	}

	var patterns []string
	for _, g := range in.Include {
		if strings.TrimSpace(g) == "" {
			continue
		}
		pattern, err := bundlePath(root, abs(g))
		if err != nil {
			return nil, refuse(CodePathInvalid, "include %q is outside the bundle root %s", g, root)
		}
		if !doublestar.ValidatePattern(pattern) {
			return nil, refuse(CodePathInvalid, "include %q is not a valid glob", g)
		}
		patterns = append(patterns, pattern)
	}

	walkRoot := entryDir
	if len(patterns) > 0 {
		walkRoot = root
	}
	set := &Set{Root: root, Entrypoint: relEntry, Files: map[string][]byte{}}

	err = filepath.WalkDir(walkRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := bundlePath(root, p)
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p == walkRoot {
				return nil
			}
			if strings.HasPrefix(d.Name(), ".") || ignore.ignored(rel, true) {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(d.Name(), ".") || hasDotSegment(rel) {
			return nil
		}
		if ignore.ignored(rel, false) {
			return nil
		}
		if len(patterns) > 0 && !matchAny(patterns, rel) {
			return nil
		}
		if d.Type()&fs.ModeSymlink != 0 {
			return refuse(CodePathInvalid, "%s is a symlink; bundles never follow symlinks (copy the file or include its target)", rel)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		return set.add(rel, p)
	})
	if err != nil {
		return nil, err
	}

	// The entrypoint and the session setup file next to it are always part
	// of the set, even when an `include` glob or an ignore rule leaves them
	// out.
	if err := set.add(relEntry, entry); err != nil {
		return nil, err
	}
	setup := filepath.Join(entryDir, DefaultSetupFile)
	if st, err := os.Lstat(setup); err == nil && st.Mode().IsRegular() {
		rel, err := bundlePath(root, setup)
		if err == nil {
			if err := set.add(rel, setup); err != nil {
				return nil, err
			}
		}
	}

	if err := set.checkLimits(); err != nil {
		return nil, err
	}
	return set, nil
}

func (s *Set) add(rel, abs string) error {
	if _, ok := s.Files[rel]; ok {
		return nil
	}
	content, err := os.ReadFile(abs)
	if err != nil {
		return fmt.Errorf("reading %s: %w", rel, err)
	}
	s.Files[rel] = content
	return nil
}

func (s *Set) checkLimits() error {
	total := 0
	for _, c := range s.Files {
		total += len(c)
	}
	if len(s.Files) <= MaxFiles && total <= MaxBytes {
		return nil
	}
	return refuse(CodeTooLarge, "bundle has %d files and %d bytes; the limit is %d files and %d bytes. Largest directories: %s. "+
		"Narrow code.include or add a %s", len(s.Files), total, MaxFiles, MaxBytes, strings.Join(s.largestDirectories(), ", "), IgnoreFile)
}

// largestDirectories lists the 10 largest top-level directories, the same
// grouping the server reports for BUNDLE_TOO_LARGE.
func (s *Set) largestDirectories() []string {
	sizes := map[string]int{}
	for p, c := range s.Files {
		top := "(root)"
		if i := strings.IndexByte(p, '/'); i >= 0 {
			top = p[:i]
		}
		sizes[top] += len(c)
	}
	dirs := make([]string, 0, len(sizes))
	for d := range sizes {
		dirs = append(dirs, d)
	}
	sort.Slice(dirs, func(i, j int) bool {
		if sizes[dirs[i]] != sizes[dirs[j]] {
			return sizes[dirs[i]] > sizes[dirs[j]]
		}
		return dirs[i] < dirs[j]
	})
	if len(dirs) > 10 {
		dirs = dirs[:10]
	}
	out := make([]string, len(dirs))
	for i, d := range dirs {
		out[i] = fmt.Sprintf("%s (%d bytes)", d, sizes[d])
	}
	return out
}

// NormalizePath applies the D-12 path rules: POSIX separators, NFC, no
// leading `./`.
func NormalizePath(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	for strings.HasPrefix(p, "./") {
		p = p[2:]
	}
	return norm.NFC.String(p)
}

// bundlePath returns target relative to root as a normalized bundle path,
// or an error when target is outside root.
func bundlePath(root, target string) (string, error) {
	rel, err := filepath.Rel(root, target)
	if err != nil {
		return "", err
	}
	rel = filepath.ToSlash(rel)
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", errors.New("outside root")
	}
	if rel == "." {
		return "", nil
	}
	return NormalizePath(rel), nil
}

func hasDotSegment(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

func matchAny(patterns []string, rel string) bool {
	for _, p := range patterns {
		if ok, _ := doublestar.Match(p, rel); ok {
			return true
		}
		// `dir` and `dir/` include the directory's contents.
		trimmed := strings.TrimSuffix(p, "/")
		if rel == trimmed || strings.HasPrefix(rel, trimmed+"/") && !strings.ContainsAny(trimmed, "*?[{") {
			return true
		}
	}
	return false
}

// findGitRoot returns the nearest ancestor of dir (inclusive) that contains
// a `.git` entry, or "".
func findGitRoot(dir string) string {
	for {
		if _, err := os.Lstat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func uniqueDirs(dirs ...string) []string {
	seen := map[string]bool{}
	var out []string
	for _, d := range dirs {
		if !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

func unwrapPathError(err error) error {
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err
	}
	return err
}

// ── .devhelmignore (gitignore syntax) ──────────────────────────────────

type ignoreRule struct {
	pattern string // doublestar pattern over bundle paths
	negate  bool
	dirOnly bool
}

type ignoreRules struct{ rules []ignoreRule }

func newIgnoreRules(lines []string, base string) *ignoreRules {
	r := &ignoreRules{}
	r.addLines(lines, base)
	return r
}

// loadFile reads dir/.devhelmignore (if present); its patterns are relative
// to dir, which is root or below it.
func (r *ignoreRules) loadFile(root, dir string) error {
	raw, err := os.ReadFile(filepath.Join(dir, IgnoreFile))
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("reading %s: %w", IgnoreFile, err)
	}
	base, err := bundlePath(root, dir)
	if err != nil {
		return nil
	}
	r.addLines(strings.Split(string(raw), "\n"), base)
	return nil
}

func (r *ignoreRules) addLines(lines []string, base string) {
	for _, line := range lines {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		line = strings.TrimRight(line, " ")
		rule := ignoreRule{}
		if strings.HasPrefix(line, "!") {
			rule.negate = true
			line = line[1:]
		}
		if strings.HasSuffix(line, "/") {
			rule.dirOnly = true
			line = strings.TrimSuffix(line, "/")
		}
		anchored := strings.HasPrefix(line, "/") || strings.Contains(line, "/")
		line = strings.TrimPrefix(line, "/")
		if line == "" {
			continue
		}
		if !anchored {
			line = "**/" + line
		}
		if base != "" {
			line = path.Join(base, line)
		}
		rule.pattern = line
		r.rules = append(r.rules, rule)
	}
}

// ignored applies the rules in order; the last matching rule wins. A file
// under an ignored directory is ignored as well (the walk prunes those
// directories before reaching their files).
func (r *ignoreRules) ignored(rel string, isDir bool) bool {
	out := false
	for _, rule := range r.rules {
		if rule.dirOnly && !isDir {
			continue
		}
		if ok, _ := doublestar.Match(rule.pattern, rel); ok {
			out = !rule.negate
		}
	}
	return out
}
