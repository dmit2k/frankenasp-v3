package asp

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
	Mu     sync.Mutex
	phase  string
	Active bool
	Drawn  bool
	scans  int
	Tty    bool
	width  int
	Out    io.Writer
	Done   chan struct{}
	once   sync.Once
}

func newMeter() *progressMeter {
	_, err := unix.IoctlGetTermios(int(os.Stderr.Fd()), unix.TCGETS)
	m := &progressMeter{Tty: err == nil, width: 80, Out: os.Stderr, Done: make(chan struct{})}
	if m.Tty {
		if ws, e := unix.IoctlGetWinsize(int(os.Stderr.Fd()), unix.TIOCGWINSZ); e == nil && ws.Col > 0 {
			m.width = int(ws.Col)
		}
		go m.spin()
	}
	return m
}
func (m *progressMeter) clearLocked() {
	if m.Tty && m.Drawn {
		fmt.Fprint(m.Out, "\r\x1b[2K")
	}
	m.Drawn = false
}
func (m *progressMeter) spin() {
	tick := time.NewTicker(130 * time.Millisecond)
	defer tick.Stop()
	marks := []string{"|", "/", "-", "\\"}
	step := 0
	for {
		select {
		case <-m.Done:
			return
		case <-tick.C:
			m.Mu.Lock()
			if m.Active && m.Tty {
				label := "asp-installer " + marks[step%len(marks)] + " " + m.phase
				chars := []rune(label)
				limit := m.width - 2
				if limit < 12 {
					limit = 12
				}
				if len(chars) > limit {
					label = string(chars[:limit-3]) + "..."
				}
				fmt.Fprintf(m.Out, "\r\x1b[2K%s", label)
				m.Drawn = true
				step++
			}
			m.Mu.Unlock()
		}
	}
}
func (m *progressMeter) Phase(label string) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.clearLocked()
	m.phase = label
	m.Active = true
	fmt.Fprintln(m.Out, "asp-installer: "+label+" ...")
}

// NextScan gives every partition stream a shared, strictly increasing display ID.
func (m *progressMeter) NextScan() int {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.scans++
	return m.scans
}
func (m *progressMeter) Note(message string) {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.clearLocked()
	fmt.Fprintln(m.Out, "asp-installer: "+strings.TrimSpace(message))
}
func (m *progressMeter) Stop() {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	m.Active = false
	m.clearLocked()
}
func (m *progressMeter) Close() {
	m.Stop()
	m.once.Do(func() { close(m.Done) })
}
