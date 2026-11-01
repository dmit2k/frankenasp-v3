package main

import (
 "fmt"
 "os"
 "strings"
 "sync"
 "time"

 "golang.org/x/sys/unix"
)

// Progress is written only to stderr; JSON results remain on stdout.
type progressMeter struct {
 mu sync.Mutex
 phase string
 active bool
 tty bool
 done chan struct{}
}
func newMeter() *progressMeter {
 _,err:=unix.IoctlGetTermios(int(os.Stderr.Fd()),unix.TCGETS)
 m:=&progressMeter{tty:err==nil,done:make(chan struct{})}
 if m.tty {go m.spin()}
 return m
}
func(m *progressMeter)spin() {
 ticker:=time.NewTicker(130*time.Millisecond)
 defer ticker.Stop()
 marks:=[]string{"|","/","-","."}
 step:=0
 for {
  select {
  case <-m.done: return
  case <-ticker.C:
   m.mu.Lock()
   if m.active {fmt.Fprintf(os.Stderr,"\rasp-installer %s %s",marks[step%len(marks)],m.phase);step++}
   m.mu.Unlock()
  }
 }
}
func(m *progressMeter)Phase(label string) {
 m.mu.Lock()
 defer m.mu.Unlock()
 if m.tty {fmt.Fprint(os.Stderr,"\r")}
 m.phase=label
 m.active=true
 fmt.Fprintln(os.Stderr,"asp-installer: "+label+" ...")
}
func(m *progressMeter)Note(message string) {
 m.mu.Lock()
 defer m.mu.Unlock()
 if m.tty {fmt.Fprint(os.Stderr,"\r")}
 fmt.Fprintln(os.Stderr,"asp-installer: "+strings.TrimSpace(message))
}
func(m *progressMeter)Close() {
 m.mu.Lock()
 m.active=false
 m.mu.Unlock()
 close(m.done)
}
