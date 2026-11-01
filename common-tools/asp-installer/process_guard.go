package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

type fileID struct {
	dev uint64
	ino uint64
}

func idOf(path string) (fileID, error) {
	var st unix.Stat_t
	if e := unix.Stat(path, &st); e != nil {
		return fileID{}, e
	}
	return fileID{uint64(st.Dev), st.Ino}, nil
}

type processProof struct {
	pid    int
	start  string
	exe    fileID
	parent int
	role   string
}
type activity struct {
	helpers  []processProof
	blockers []string
}

func statIdentity(pid int) (string, int, error) {
	b, e := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if e != nil {
		return "", 0, e
	}
	cut := strings.LastIndexByte(string(b), ')')
	if cut < 0 {
		return "", 0, errors.New("invalid /proc stat")
	}
	words := strings.Fields(string(b[cut+1:]))
	if len(words) < 20 {
		return "", 0, errors.New("short /proc stat")
	}
	parent, e := strconv.Atoi(words[1])
	if e != nil {
		return "", 0, e
	}
	return words[19], parent, nil
}
func processInfo(pid int) (processProof, error) {
	start, parent, e := statIdentity(pid)
	if e != nil {
		return processProof{}, e
	}
	exe, e := idOf(fmt.Sprintf("/proc/%d/exe", pid))
	if e != nil {
		return processProof{}, e
	}
	end, endParent, e := statIdentity(pid)
	if e != nil || start != end || parent != endParent {
		return processProof{}, errors.New("process changed during inspection")
	}
	return processProof{pid: pid, start: start, parent: parent, exe: exe}, nil
}
func insideAny(path string, roots []string) bool {
	path = strings.TrimSuffix(path, " (deleted)")
	for _, root := range roots {
		if path == root || strings.HasPrefix(path, root+"/") {
			return true
		}
	}
	return false
}
func boundedProc(path string, maxBytes int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	s, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if s.Size() > maxBytes {
		return nil, errors.New("oversized /proc data")
	}
	b, e := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if e != nil {
		return nil, e
	}
	if int64(len(b)) > maxBytes {
		return nil, errors.New("oversized /proc data")
	}
	return b, nil
}
func activeRoots() ([]string, error) {
	roots := []string{}
	for _, path := range append([]string{destination}, knownGenerations()...) {
		fi, e := os.Lstat(path)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return nil, e
		}
		if !fi.IsDir() || fi.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("unsafe installed root %s", path)
		}
		roots = append(roots, path)
	}
	return roots, nil
}
func scanActivity(roots []string) (activity, error) {
	report := activity{helpers: []processProof{}, blockers: []string{}}
	trusted := map[fileID]string{}
	for _, root := range roots {
		for relative, role := range map[string]string{"bin/external-asp": "launcher", "lib/rootfs/factory/asp1-child": "helper"} {
			id, e := idOf(filepath.Join(root, relative))
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return report, e
			}
			trusted[id] = role
		}
	}
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return report, e
	}
	if len(entries) > 32768 {
		return report, errors.New("too many /proc entries")
	}
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || pid == os.Getpid() {
			continue
		}
		proof, e := processInfo(pid)
		if os.IsNotExist(e) || errors.Is(e, unix.ESRCH) {
			continue
		}
		if e != nil {
			return report, fmt.Errorf("unsafe /proc/%d state: %w", pid, e)
		}
		if role, ok := trusted[proof.exe]; ok {
			proof.role = role
			report.helpers = append(report.helpers, proof)
			continue
		}
		// Never identify authority by a process name. Unrelated references block mutation.
		prefix := fmt.Sprintf("/proc/%d/", pid)
		for _, name := range []string{"exe", "cwd", "root"} {
			link, e := os.Readlink(prefix + name)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return report, fmt.Errorf("cannot read process %d %s: %w", pid, name, e)
			}
			if insideAny(link, roots) {
				report.blockers = append(report.blockers, fmt.Sprintf("PID %d retains %s", pid, name))
			}
		}
		fds, e := os.ReadDir(prefix + "fd")
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return report, fmt.Errorf("cannot inspect PID %d descriptors: %w", pid, e)
		}
		if len(fds) > 8192 {
			return report, errors.New("oversized process FD table")
		}
		for _, fd := range fds {
			link, e := os.Readlink(prefix + "fd/" + fd.Name())
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return report, fmt.Errorf("unreadable process %d FD: %w", pid, e)
			}
			if insideAny(link, roots) {
				report.blockers = append(report.blockers, fmt.Sprintf("PID %d retains FD %s", pid, fd.Name()))
			}
		}
		for _, name := range []string{"maps", "mountinfo"} {
			b, e := boundedProc(prefix+name, 8<<20)
			if os.IsNotExist(e) {
				continue
			}
			if e != nil {
				return report, fmt.Errorf("PID %d %s unavailable: %w", pid, name, e)
			}
			for _, root := range roots {
				if strings.Contains(string(b), root) {
					report.blockers = append(report.blockers, fmt.Sprintf("PID %d references runtime in %s", pid, name))
					break
				}
			}
		}
	}
	return report, nil
}
func verifiedSignal(proof processProof, signal syscall.Signal) error {
	current, e := processInfo(proof.pid)
	if e != nil {
		return e
	}
	if current.start != proof.start || current.exe != proof.exe || current.parent != proof.parent {
		return errors.New("process identity changed before signal")
	}
	fd, e := unix.PidfdOpen(proof.pid, 0)
	if e == nil {
		defer unix.Close(fd)
		return unix.PidfdSendSignal(fd, signal, nil, 0)
	}
	if e != unix.ENOSYS && e != unix.EINVAL {
		return e
	}
	// Old kernels: verify again immediately, no process-name-based kill.
	current, e = processInfo(proof.pid)
	if e != nil {
		return e
	}
	if current.start != proof.start || current.exe != proof.exe || current.parent != proof.parent {
		return errors.New("process PID reused")
	}
	return unix.Kill(proof.pid, signal)
}
func stopVerifiedHelpers(roots []string, m *progressMeter) error {
	report, e := scanActivity(roots)
	if e != nil {
		return e
	}
	if len(report.helpers) == 0 {
		return nil
	}
	m.Note(fmt.Sprintf("Found %d verified FrankenASP processes; sending SIGTERM (never killing TECHO5 by name)", len(report.helpers)))
	for _, p := range report.helpers {
		if e := verifiedSignal(p, syscall.SIGTERM); e != nil && e != unix.ESRCH && !os.IsNotExist(e) {
			return fmt.Errorf("PID %d: %w", p.pid, e)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		current, e := scanActivity(roots)
		if e != nil {
			return e
		}
		if len(current.helpers) == 0 {
			return nil
		}
		time.Sleep(150 * time.Millisecond)
	}
	return errors.New("verified helper still running or respawned; disable External ASP in TECHO5 and retry")
}
func requireInactive(roots []string, m *progressMeter, restart bool) error {
	if e := stopVerifiedHelpers(roots, m); e != nil {
		return e
	}
	activity, e := scanActivity(roots)
	if e != nil {
		return e
	}
	if len(activity.helpers) > 0 {
		return errors.New("external helper respawned; disable external ASP in TECHO5")
	}
	if len(activity.blockers) == 0 {
		return nil
	}
	if !restart {
		return fmt.Errorf("running process still pins external-asp (%s); disable External ASP and retry, or explicitly --restart-techo5", strings.Join(activity.blockers, "; "))
	}
	m.Note("Verified runtime still in use; checking whether TECHO5 can be restarted safely")
	if e := restartVerifiedTecho5(m); e != nil {
		return e
	}
	activity, e = scanActivity(roots)
	if e != nil {
		return e
	}
	if len(activity.helpers) > 0 || len(activity.blockers) > 0 {
		return fmt.Errorf("runtime remains referenced after TECHO5 restart; no mutation (%s)", strings.Join(activity.blockers, "; "))
	}
	return nil
}
func restartVerifiedTecho5(m *progressMeter) error {
	// Linux TECHO5 runs under BusyBox init (inittab respawn). We must positively
	// identify that exact binary and PID1 parent; no killall/pkill/systemctl.
	path := "/usr/local/bin/techo5"
	trusted, e := idOf(path)
	if e != nil {
		return fmt.Errorf("cannot prove supervised TECHO5 executable %s: %w", path, e)
	}
	entries, e := os.ReadDir("/proc")
	if e != nil {
		return e
	}
	var match []processProof
	for _, entry := range entries {
		pid, e := strconv.Atoi(entry.Name())
		if e != nil || pid <= 1 || pid == os.Getpid() {
			continue
		}
		proof, e := processInfo(pid)
		if os.IsNotExist(e) {
			continue
		}
		if e != nil {
			return e
		}
		if proof.exe == trusted {
			if proof.parent != 1 {
				return errors.New("TECHO5 process is not directly supervised by PID 1; refusing restart")
			}
			match = append(match, proof)
		}
	}
	if len(match) != 1 {
		return fmt.Errorf("expected exactly one supervised TECHO5 process, found %d; refusing restart", len(match))
	}
	m.Note(fmt.Sprintf("Restarting verified supervised TECHO5 PID %d", match[0].pid))
	if e := verifiedSignal(match[0], syscall.SIGTERM); e != nil {
		return e
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		entries, _ := os.ReadDir("/proc")
		for _, entry := range entries {
			pid, e := strconv.Atoi(entry.Name())
			if e != nil || pid <= 1 || pid == match[0].pid {
				continue
			}
			proof, e := processInfo(pid)
			if e == nil && proof.exe == trusted && proof.parent == 1 {
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("TECHO5 respawn not observed; inspect device manually")
}
