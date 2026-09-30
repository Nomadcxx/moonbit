package paths

import (
	"errors"
	"os"
	"os/user"
	"path/filepath"
	"syscall"
	"testing"
)

// HOME-1: resolving SUDO_USER must look the user up rather than assuming
// /home/<name>. On a non-standard layout the old code fell through to HOME,
// which under sudo's env_reset is /root -- so moonbit scanned and cleaned root's
// caches while reporting success.
//
// The SUDO_USER branch only runs as euid 0, so assert the lookup mechanism the
// fix depends on and the precedence rules that are testable unprivileged.
func TestSudoUserHomeResolutionUsesUserLookup(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("test asserts unprivileged precedence; see the euid-0 branch separately")
	}

	current, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}

	// The mechanism the fix relies on: user.Lookup yields the real home, which
	// need not be /home/<name>.
	looked, err := user.Lookup(current.Username)
	if err != nil {
		t.Skipf("user.Lookup unavailable: %v", err)
	}
	if looked.HomeDir == "" {
		t.Fatal("user.Lookup returned an empty home directory")
	}
	if looked.HomeDir != current.HomeDir {
		t.Errorf("user.Lookup disagrees with user.Current: %q vs %q",
			looked.HomeDir, current.HomeDir)
	}

	// Not asserting the value equals /home/<name> -- that assumption is the bug.
	t.Logf("resolved home for %s: %s (assumed /home path would be %s)",
		current.Username, looked.HomeDir, filepath.Join("/home", current.Username))
}

// MOONBIT_HOME is the documented escape hatch for ordinary runs.
func TestMoonbitHomeOverrides(t *testing.T) {
	want := t.TempDir()
	t.Setenv("MOONBIT_HOME", want)
	t.Setenv("HOME", "/definitely/not/this")
	t.Setenv("SUDO_USER", "")
	t.Setenv("PKEXEC_UID", "")

	got, err := HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("MOONBIT_HOME should take precedence when not elevated: got %q, want %q", got, want)
	}
}

func TestElevatedHomeIgnoresMoonbitHomeOverride(t *testing.T) {
	if os.Geteuid() != 0 {
		t.Skip("requires root to exercise elevated home resolution")
	}
	current, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}
	want := homeFromPasswd(user.Lookup, current.Username)
	if want == "" {
		t.Skip("current user has no resolvable home")
	}
	t.Setenv("SUDO_USER", current.Username)
	t.Setenv("PKEXEC_UID", "")
	t.Setenv("MOONBIT_HOME", t.TempDir())

	got, err := HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("elevated HomeDir() = %q, want invoking user's home %q", got, want)
	}
}

// Unprivileged and with no SUDO_USER, HOME is authoritative.
func TestHomeEnvUsedWhenNotElevated(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged execution")
	}
	want := t.TempDir()
	t.Setenv("MOONBIT_HOME", "")
	t.Setenv("SUDO_USER", "")
	t.Setenv("HOME", want)

	got, err := HomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// The XDG variables the systemd units set must redirect every moonbit path, so
// automation never writes into a user's home.
func TestXDGOverridesRedirectAllPaths(t *testing.T) {
	base := t.TempDir()
	t.Setenv("MOONBIT_HOME", filepath.Join(base, "home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(base, "cfg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(base, "cache"))
	t.Setenv("XDG_DATA_HOME", filepath.Join(base, "data"))

	cfg, err := ConfigFile()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "cfg", "moonbit", "config.toml"); cfg != want {
		t.Errorf("ConfigFile: got %q, want %q", cfg, want)
	}

	cache, err := CacheFile()
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "cache", "moonbit", "scan_results.json"); cache != want {
		t.Errorf("CacheFile: got %q, want %q", cache, want)
	}

	data, err := DataDir("backups")
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(base, "data", "moonbit", "backups"); data != want {
		t.Errorf("DataDir: got %q, want %q", data, want)
	}
}

// The desktop launcher runs moonbit through pkexec, which sets PKEXEC_UID and
// never SUDO_USER. Without that branch HomeDir falls through to HOME, which is
// /root under an elevation, and moonbit cleans root's caches while telling the
// user it cleaned theirs.
func TestPkexecUidIsResolvedThroughPasswd(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("asserts the unprivileged precedence; the branch itself needs euid 0")
	}

	current, err := user.Current()
	if err != nil {
		t.Skipf("cannot determine current user: %v", err)
	}

	// The mechanism the fix relies on: a numeric uid resolves to a home.
	looked, err := user.LookupId(current.Uid)
	if err != nil {
		t.Skipf("user.LookupId unavailable: %v", err)
	}
	if looked.HomeDir == "" {
		t.Fatal("user.LookupId returned an empty home directory")
	}
	if looked.HomeDir != current.HomeDir {
		t.Errorf("LookupId disagrees with Current: %q vs %q", looked.HomeDir, current.HomeDir)
	}

	// homeFromPasswd is what the PKEXEC_UID branch calls.
	if got := homeFromPasswd(user.LookupId, current.Uid); got != current.HomeDir {
		t.Errorf("homeFromPasswd(uid=%s) = %q, want %q", current.Uid, got, current.HomeDir)
	}
	// An unknown uid must fail rather than guessing.
	if got := homeFromPasswd(user.LookupId, "4294967290"); got != "" {
		t.Errorf("unknown uid should not resolve, got %q", got)
	}
}

func TestReadDirRejectsSymlinkedDirectory(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	link := filepath.Join(base, "link")
	if err := os.Mkdir(outside, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "secret.backup"), []byte(""), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if _, err := ReadDir(link); err == nil {
		t.Fatal("ReadDir should reject a symlinked directory")
	}
}

func TestHomeFromPasswdRejectsMissingUser(t *testing.T) {
	if got := homeFromPasswd(user.Lookup, "definitely-not-a-real-user-9f3a"); got != "" {
		t.Errorf("unknown user should not resolve, got %q", got)
	}
}

func TestChmodDirRejectsSymlinkAndChangesDirectoryMode(t *testing.T) {
	base := t.TempDir()
	actual := filepath.Join(base, "actual")
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(actual, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(outside, 0755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	if err := ChmodDir(actual, 0700); err != nil {
		t.Fatal(err)
	}
	actualInfo, err := os.Stat(actual)
	if err != nil {
		t.Fatal(err)
	}
	if actualInfo.Mode().Perm() != 0700 {
		t.Fatalf("directory mode = %04o, want 0700", actualInfo.Mode().Perm())
	}
	if err := ChmodDir(link, 0700); err == nil {
		t.Fatal("expected symlinked directory to be rejected")
	}
	outsideInfo, err := os.Stat(outside)
	if err != nil {
		t.Fatal(err)
	}
	if outsideInfo.Mode().Perm() != 0755 {
		t.Fatalf("symlink target mode = %04o, want unchanged 0755", outsideInfo.Mode().Perm())
	}
}

func TestAtomicWriteFilePreservesDestinationOnWriteError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.toml")
	original := []byte("keep the previous file")
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}

	err := AtomicWriteFile(path, 0644, func(file *os.File) error {
		if _, err := file.Write([]byte("partial replacement")); err != nil {
			return err
		}
		return errors.New("simulated encoder failure")
	})
	if err == nil {
		t.Fatal("expected write callback error")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(original) {
		t.Fatalf("destination changed after failed write: %q", got)
	}
}

func TestAtomicWriteFileRejectsSymlinkAndFIFO(t *testing.T) {
	base := t.TempDir()
	outside := filepath.Join(base, "outside")
	if err := os.WriteFile(outside, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	symlink := filepath.Join(base, "config-link")
	if err := os.Symlink(outside, symlink); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}
	if err := AtomicWriteFile(symlink, 0600, func(file *os.File) error {
		_, err := file.Write([]byte("replacement"))
		return err
	}); err == nil {
		t.Fatal("expected symlink destination to be rejected")
	}
	got, err := os.ReadFile(outside)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep" {
		t.Fatalf("symlink target changed: %q", got)
	}

	fifo := filepath.Join(base, "config-fifo")
	if err := syscall.Mkfifo(fifo, 0600); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFile(fifo, 0600, func(file *os.File) error {
		_, err := file.Write([]byte("must not reach fifo"))
		return err
	}); err == nil {
		t.Fatal("expected FIFO destination to be rejected")
	}
}

func TestRemoveIfLeavesFileWhenValidationFails(t *testing.T) {
	path := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(path, []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	wantErr := errors.New("identity changed")
	err := RemoveIf(path, func(info os.FileInfo) error {
		if !info.Mode().IsRegular() {
			t.Errorf("opened target mode = %v, want regular file", info.Mode())
		}
		return wantErr
	})
	if !errors.Is(err, wantErr) {
		t.Fatalf("RemoveIf error = %v, want %v", err, wantErr)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("rejected target was removed: %v", err)
	}
}

func TestRemoveIfCanRemoveReadOnlyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readonly")
	if err := os.WriteFile(path, []byte("remove"), 0400); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0400); err != nil {
		t.Fatal(err)
	}
	if err := RemoveIf(path, func(os.FileInfo) error { return nil }); err != nil {
		t.Fatalf("RemoveIf failed for read-only file: %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("read-only file still exists: %v", err)
	}
}

func TestIsWithinUsesPathComponentBoundaries(t *testing.T) {
	root := filepath.Join(t.TempDir(), "cache")
	tests := []struct {
		path string
		want bool
	}{
		{root, true},
		{filepath.Join(root, "nested", "file"), true},
		{root + "-old/file", false},
		{filepath.Dir(root), false},
	}
	for _, tt := range tests {
		if got := IsWithin(root, tt.path); got != tt.want {
			t.Errorf("IsWithin(%q, %q) = %v, want %v", root, tt.path, got, tt.want)
		}
	}
	if IsWithin("", root) {
		t.Fatal("empty root must not contain a path")
	}
}

func TestMatchesPathOrDescendantUsesGlobRootsAndComponentBoundaries(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "app-one", "cache")
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	pattern := filepath.Join(base, "app-*", "cache")
	tests := []struct {
		name string
		path string
		want bool
	}{
		{name: "glob root", path: root, want: true},
		{name: "glob descendant", path: filepath.Join(root, "nested", "file"), want: true},
		{name: "similarly prefixed sibling", path: root + "-old/file", want: false},
		{name: "outside sibling", path: filepath.Join(base, "app-two", "cache-old/file"), want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MatchesPathOrDescendant(pattern, tt.path); got != tt.want {
				t.Fatalf("MatchesPathOrDescendant(%q, %q) = %v, want %v", pattern, tt.path, got, tt.want)
			}
		})
	}
}
