package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

const miccalKernelRoot = "/sys/firmware/devicetree/base/idme"

// discoverMiccalAt inspects the genuine kernel property layout through pinned
// directory descriptors. It reads one byte only to confirm that a property
// contains data. Calibration coefficients are never copied, hashed or logged.
func discoverMiccalAt(root string, requiredUID uint32) ([]int, error) {
	rootfd, err := pinnedMiccalRoot(root)
	if err != nil {
		return nil, err
	}
	defer unix.Close(rootfd)
	var st unix.Stat_t
	if err = unix.Fstat(rootfd, &st); err != nil {
		return nil, err
	}
	if st.Mode&unix.S_IFMT != unix.S_IFDIR || st.Uid != requiredUID {
		return nil, errors.New("untrusted kernel calibration directory")
	}
	fd, err := unix.Dup(rootfd)
	if err != nil {
		return nil, err
	}
	dir := os.NewFile(uintptr(fd), "idme")
	names, e := dir.Readdirnames(1025)
	_ = dir.Close()
	if e != nil && e != io.EOF {
		return nil, e
	}
	if len(names) > 1024 {
		return nil, errors.New("kernel calibration directory entries exceeded")
	}
	ids := make([]int, 0)
	for _, name := range names {
		if !strings.HasPrefix(name, "miccal.") {
			continue
		}
		idx := strings.TrimPrefix(name, "miccal.")
		n, e := strconv.Atoi(idx)
		if e != nil || n < 0 || n > 255 || strconv.Itoa(n) != idx {
			return nil, errors.New("suspicious miccal node name")
		}
		node, e := unix.Openat(rootfd, name, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if e != nil {
			return nil, fmt.Errorf("untrusted miccal node %s: %w", name, e)
		}
		var nodestat unix.Stat_t
		e = unix.Fstat(node, &nodestat)
		if e != nil || nodestat.Mode&unix.S_IFMT != unix.S_IFDIR || nodestat.Uid != requiredUID {
			unix.Close(node)
			return nil, fmt.Errorf("untrusted miccal node %s", name)
		}
		property, e := unix.Openat(node, "value", unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		unix.Close(node)
		if e != nil {
			return nil, fmt.Errorf("miccal property inaccessible %s: %w", name, e)
		}
		var propstat unix.Stat_t
		e = unix.Fstat(property, &propstat)
		if e != nil || propstat.Mode&unix.S_IFMT != unix.S_IFREG || propstat.Uid != requiredUID {
			unix.Close(property)
			return nil, fmt.Errorf("untrusted miccal value %s", name)
		}
		var sample [1]byte
		read, e := unix.Read(property, sample[:])
		unix.Close(property)
		sample[0] = 0
		if e != nil {
			return nil, fmt.Errorf("miccal property read %s: %w", name, e)
		}
		if read == 0 {
			continue
		}
		ids = append(ids, n)
		if len(ids) > 64 {
			return nil, errors.New("too many authentic miccal aliases")
		}
	}
	sort.Ints(ids)
	return ids, nil
}

func pinnedMiccalRoot(root string) (int, error) {
	if !strings.HasPrefix(root, "/") {
		return -1, errors.New("kernel idme path not absolute")
	}
	fd, e := unix.Open("/", unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
	if e != nil {
		return -1, e
	}
	for _, part := range strings.Split(strings.TrimPrefix(root, "/"), "/") {
		if part == "" {
			continue
		}
		if part == "." || part == ".." {
			unix.Close(fd)
			return -1, errors.New("unsafe device-tree path")
		}
		next, e := unix.Openat(fd, part, unix.O_RDONLY|unix.O_NOFOLLOW|unix.O_DIRECTORY|unix.O_CLOEXEC, 0)
		unix.Close(fd)
		if e != nil {
			return -1, e
		}
		fd = next
	}
	return fd, nil
}

var discoverMiccal = func() ([]int, error) { return discoverMiccalAt(miccalKernelRoot, 0) }

func aliasTargetsForMiccal(ids []int) map[string]string {
	result := map[string]string{
		"self":        "/.realproc/self",
		"sys":         "/.realproc/sys",
		"cpuinfo":     "/.realproc/cpuinfo",
		"device-tree": "/sys/firmware/devicetree/base",
	}
	for _, idx := range ids {
		result[fmt.Sprintf("idme/miccal.%d", idx)] = fmt.Sprintf("/proc/device-tree/idme/miccal.%d/value", idx)
	}
	return result
}
