package paths

import (
	cryptorand "crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

var errNotRegularFile = errors.New("not a regular file")

// FileID returns the device and inode identity for files on Linux.
func FileID(info os.FileInfo) string {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return ""
	}
	return strconv.FormatUint(uint64(stat.Dev), 16) + ":" + strconv.FormatUint(uint64(stat.Ino), 16)
}

// MkdirAll creates path components without following symbolic links.
func MkdirAll(path string, perm os.FileMode) error {
	fd, err := openDir(path, true, perm)
	if err != nil {
		return &os.PathError{Op: "mkdir", Path: path, Err: err}
	}
	return syscall.Close(fd)
}

// ChmodDir changes a directory's permissions without following symbolic links.
func ChmodDir(path string, perm os.FileMode) error {
	fd, err := openDir(path, false, 0)
	if err != nil {
		return &os.PathError{Op: "chmod", Path: path, Err: err}
	}
	chmodErr := syscall.Fchmod(fd, uint32(perm.Perm()))
	closeErr := syscall.Close(fd)
	if chmodErr != nil {
		return &os.PathError{Op: "chmod", Path: path, Err: chmodErr}
	}
	if closeErr != nil {
		return &os.PathError{Op: "close directory", Path: path, Err: closeErr}
	}
	return nil
}

// OpenFile opens a file without following symbolic links in its path.
func OpenFile(path string, flag int, perm os.FileMode) (*os.File, error) {
	if path == "" {
		return nil, &os.PathError{Op: "open", Path: path, Err: syscall.ENOENT}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	name := filepath.Base(abs)
	if name == "." || name == string(filepath.Separator) {
		return nil, &os.PathError{Op: "open", Path: path, Err: syscall.EISDIR}
	}

	dirFD, err := openDir(filepath.Dir(abs), false, 0)
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	defer syscall.Close(dirFD)

	fd, err := syscall.Openat(dirFD, name,
		flag&^syscall.O_TRUNC|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK,
		uint32(perm.Perm()))
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	file := os.NewFile(uintptr(fd), path)
	info, err := file.Stat()
	if err != nil {
		file.Close()
		return nil, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	if !info.Mode().IsRegular() {
		file.Close()
		return nil, &os.PathError{Op: "open", Path: path, Err: errNotRegularFile}
	}
	if flag&syscall.O_TRUNC != 0 {
		if err := file.Truncate(0); err != nil {
			file.Close()
			return nil, &os.PathError{Op: "truncate", Path: path, Err: err}
		}
	}
	return file, nil
}

// Remove unlinks path without following symbolic links in its parent path.
func Remove(path string) error {
	if path == "" {
		return &os.PathError{Op: "remove", Path: path, Err: syscall.ENOENT}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return &os.PathError{Op: "remove", Path: path, Err: err}
	}
	name := filepath.Base(abs)
	if name == "." || name == string(filepath.Separator) {
		return &os.PathError{Op: "remove", Path: path, Err: syscall.EISDIR}
	}
	dirFD, err := openDir(filepath.Dir(abs), false, 0)
	if err != nil {
		return &os.PathError{Op: "remove", Path: path, Err: err}
	}
	defer syscall.Close(dirFD)
	if err := syscall.Unlinkat(dirFD, name); err != nil {
		return &os.PathError{Op: "remove", Path: path, Err: err}
	}
	return nil
}

// ReadDir lists entries without following symlinks in the directory path.
func ReadDir(path string) ([]os.DirEntry, error) {
	fd, err := openDir(path, false, 0)
	if err != nil {
		return nil, &os.PathError{Op: "readdir", Path: path, Err: err}
	}
	dir := os.NewFile(uintptr(fd), path)
	entries, readErr := dir.ReadDir(-1)
	closeErr := dir.Close()
	if readErr != nil {
		return nil, &os.PathError{Op: "readdir", Path: path, Err: readErr}
	}
	if closeErr != nil {
		return nil, &os.PathError{Op: "close directory", Path: path, Err: closeErr}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	return entries, nil
}

// AtomicWriteFile writes through a temporary regular file in the destination
// directory, then replaces the destination with one rename. A failed write
// leaves any existing destination untouched.
func AtomicWriteFile(path string, perm os.FileMode, write func(*os.File) error) error {
	if path == "" || write == nil {
		return &os.PathError{Op: "write", Path: path, Err: syscall.EINVAL}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return &os.PathError{Op: "write", Path: path, Err: err}
	}
	name := filepath.Base(abs)
	if name == "." || name == string(filepath.Separator) {
		return &os.PathError{Op: "write", Path: path, Err: syscall.EISDIR}
	}
	dirFD, err := openDir(filepath.Dir(abs), false, 0)
	if err != nil {
		return &os.PathError{Op: "open directory", Path: path, Err: err}
	}
	defer syscall.Close(dirFD)

	existingFD, err := syscall.Openat(dirFD, name,
		syscall.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK, 0)
	if err == nil {
		var existing syscall.Stat_t
		statErr := syscall.Fstat(existingFD, &existing)
		closeErr := syscall.Close(existingFD)
		if statErr != nil {
			return &os.PathError{Op: "stat destination", Path: path, Err: statErr}
		}
		if closeErr != nil {
			return &os.PathError{Op: "close destination", Path: path, Err: closeErr}
		}
		if existing.Mode&syscall.S_IFMT != syscall.S_IFREG {
			return &os.PathError{Op: "write", Path: path, Err: errNotRegularFile}
		}
	} else if !errors.Is(err, syscall.ENOENT) {
		return &os.PathError{Op: "open destination", Path: path, Err: err}
	}

	var tempName string
	var temp *os.File
	committed := false
	defer func() {
		if temp != nil {
			temp.Close()
		}
		if tempName != "" && !committed {
			syscall.Unlinkat(dirFD, tempName)
		}
	}()

	for attempts := 0; attempts < 10; attempts++ {
		var random [16]byte
		if _, err := cryptorand.Read(random[:]); err != nil {
			return &os.PathError{Op: "create temporary file", Path: path, Err: err}
		}
		tempName = ".moonbit-" + hex.EncodeToString(random[:]) + ".tmp"
		fd, err := syscall.Openat(dirFD, tempName,
			syscall.O_WRONLY|syscall.O_CREAT|syscall.O_EXCL|syscall.O_NOFOLLOW|syscall.O_CLOEXEC|syscall.O_NONBLOCK,
			uint32(perm.Perm()))
		if errors.Is(err, syscall.EEXIST) {
			tempName = ""
			continue
		}
		if err != nil {
			return &os.PathError{Op: "create temporary file", Path: path, Err: err}
		}
		temp = os.NewFile(uintptr(fd), filepath.Join(filepath.Dir(abs), tempName))
		break
	}
	if temp == nil {
		return &os.PathError{Op: "create temporary file", Path: path, Err: syscall.EEXIST}
	}
	if err := write(temp); err != nil {
		return fmt.Errorf("write temporary file for %s: %w", path, err)
	}
	if err := temp.Sync(); err != nil {
		return &os.PathError{Op: "sync temporary file", Path: path, Err: err}
	}
	if err := temp.Close(); err != nil {
		temp = nil
		return &os.PathError{Op: "close temporary file", Path: path, Err: err}
	}
	temp = nil
	if err := syscall.Renameat(dirFD, tempName, dirFD, name); err != nil {
		return &os.PathError{Op: "rename temporary file", Path: path, Err: err}
	}
	committed = true
	if err := syscall.Fsync(dirFD); err != nil {
		return &os.PathError{Op: "sync destination directory", Path: path, Err: err}
	}
	return nil
}

func openDir(path string, create bool, perm os.FileMode) (int, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return -1, err
	}
	fd, err := syscall.Open(string(filepath.Separator), syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return -1, err
	}

	parts := strings.Split(strings.TrimPrefix(filepath.Clean(abs), string(filepath.Separator)), string(filepath.Separator))
	for _, part := range parts {
		if part == "" || part == "." {
			continue
		}
		next, openErr := syscall.Openat(fd, part,
			syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		if errors.Is(openErr, syscall.ENOENT) && create {
			if mkdirErr := syscall.Mkdirat(fd, part, uint32(perm.Perm())); mkdirErr != nil && !errors.Is(mkdirErr, syscall.EEXIST) {
				syscall.Close(fd)
				return -1, mkdirErr
			}
			next, openErr = syscall.Openat(fd, part,
				syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
		}
		if openErr != nil {
			syscall.Close(fd)
			return -1, openErr
		}
		syscall.Close(fd)
		fd = next
	}
	return fd, nil
}
