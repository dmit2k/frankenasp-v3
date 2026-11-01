package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// All progress output is serialized through one writer; command results use stdout.
// The active spinner owns only the current terminal row and is erased before logs.
type progressMeter struct {
	mu     sync.Mutex
	phase  string
	active bool
	drawn  bool
	scans  int
	tty    bool
	width  int
	out    io.Writer
	done   chan struct{}
	once   sync.Once
}

func newMeter() *progressMeter {
	_, err := unix.IoctlGetTermios(int(os.Stderr.Fd()), unix.TCGETS)
	m := &progressMeter{tty: err == nil, width: 80, out: os.Stderr, done: make(chan struct{})}
	if m.tty {
		if ws, e := unix.IoctlGetWinsize(int(os.Stderr.Fd()), unix.TIOCGWINSZ); e == nil && ws.Col > 0 {
			m.width = int(ws.Col)
		}
		go m.spin()
	}
	return m
}
func (m *progressMeter) clearLocked() {
	if m.tty && m.drawn {
		fmt.Fprint(m.out, "\r\x1b[2K")
	}
	m.drawn = false
}
func (m *progressMeter) spin() {
	tick := time.NewTicker(130 * time.Millisecond)
	defer tick.Stop()
	marks := []string{"|", "/", "-", "\\"}
	step := 0
	for {
		select {
		case <-m.done:
			return
		case <-tick.C:
			m.mu.Lock()
			if m.active && m.tty {
				label := "asp-installer " + marks[step%len(marks)] + " " + m.phase
				chars := []rune(label)
				limit := m.width - 2
				if limit < 12 {
					limit = 12
				}
				if len(chars) > limit {
					label = string(chars[:limit-3]) + "..."
				}
				fmt.Fprintf(m.out, "\r\x1b[2K%s", label)
				m.drawn = true
				step++
			}
			m.mu.Unlock()
		}
	}
}
func (m *progressMeter) Phase(label string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearLocked()
	m.phase = label
	m.active = true
	fmt.Fprintln(m.out, "asp-installer: "+label+" ...")
}

// NextScan gives every partition stream a shared, strictly increasing display ID.
func (m *progressMeter) NextScan() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.scans++
	return m.scans
}
func (m *progressMeter) Note(message string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.clearLocked()
	fmt.Fprintln(m.out, "asp-installer: "+strings.TrimSpace(message))
}
func (m *progressMeter) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = false
	m.clearLocked()
}
func (m *progressMeter) Close() {
	m.Stop()
	m.once.Do(func() { close(m.done) })
}
