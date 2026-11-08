package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

const MiB uint64 = 1 << 20

func fakeDisk(id uint64) diskSnapshot {
	return diskSnapshot{Dev: id, Available: 1600 * MiB, BlockSize: 4096}
}
func TestUnifiedSpacePlanAllRolesSameFilesystem(t *testing.T) {
	needs, budget, e := composeSpacePlan("/cache", "/install", "/downloads", fakeDisk(1), fakeDisk(1), fakeDisk(1), runtimeCapacityBound, 20*MiB, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(needs) != 1 {
		t.Fatalf("expected one filesystem: %+v", needs)
	}
	need := needs[0]
	if need.Cache != 168*MiB || need.Rootfs != 168*MiB || need.Backup != 20*MiB || need.Reserve != 64*MiB || need.OTA != 0 {
		t.Fatalf("incorrect component totals: %+v", need)
	}
	if !strings.Contains(need.Role, "OTA download (size pending)") {
		t.Fatalf("should not imply OTA bytes known: %+v", need)
	}
	if budget.Need.Cache != 168*MiB || budget.Need.Rootfs != 168*MiB || budget.Need.Backup != 20*MiB || budget.Need.Path != "/downloads" {
		t.Fatalf("wrong download budget %+v", budget.Need)
	}
	known := budget.Need
	known.OTA = 1100 * MiB
	report, e := evaluateSpace(1600*MiB, known)
	if e != nil || report.RequiredMiB != 1520 {
		t.Fatalf("wrong actual content length total: %+v %v", report, e)
	}
	if _, e = evaluateSpace(1500*MiB, known); e == nil {
		t.Fatal("combined same-volume shortage accepted")
	}
}
func TestUnifiedSpacePlanSeparatedFilesystems(t *testing.T) {
	needs, budget, e := composeSpacePlan("/cache", "/install", "/downloads", fakeDisk(1), fakeDisk(2), fakeDisk(3), runtimeCapacityBound, 20*MiB, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(needs) != 3 {
		t.Fatalf("should check three volumes, got %d", len(needs))
	}
	if needs[0].Cache != 168*MiB || needs[0].Rootfs != 0 || needs[0].Reserve != 64*MiB {
		t.Fatalf("cache volume: %+v", needs[0])
	}
	if needs[1].Rootfs != 168*MiB || needs[1].Backup != 20*MiB || needs[1].Cache != 0 || needs[1].Reserve != 64*MiB {
		t.Fatalf("installation volume: %+v", needs[1])
	}
	if budget.Need.Rootfs != 0 || budget.Need.Cache != 0 || budget.Need.Backup != 0 || budget.Need.Reserve != 64*MiB {
		t.Fatalf("OTA download must not reserve other device's space: %+v", budget.Need)
	}
	budget.Need.OTA = 1100 * MiB
	report, e := evaluateSpace(1164*MiB, budget.Need)
	if e != nil || report.RequiredMiB != 1164 {
		t.Fatalf("OTA volume budget wrong: %+v %v", report, e)
	}
}
func TestUnifiedSpacePlanSharedCacheAndDownload(t *testing.T) {
	needs, budget, e := composeSpacePlan("/cache", "/install", "/downloads", fakeDisk(1), fakeDisk(2), fakeDisk(1), runtimeCapacityBound, 20*MiB, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(needs) != 2 || budget.Need.Cache != 168*MiB || budget.Need.Rootfs != 0 || budget.Need.Backup != 0 {
		t.Fatalf("cache+download grouping wrong: %+v %+v", needs, budget.Need)
	}
}
func TestUnifiedSpacePlanLocalOTADoesNotDuplicateFileBytes(t *testing.T) {
	needs, _, e := composeSpacePlan("/cache", "/install", "/unused", fakeDisk(1), fakeDisk(1), fakeDisk(1), runtimeCapacityBound, 0, false)
	if e != nil {
		t.Fatal(e)
	}
	if len(needs) != 1 || needs[0].OTA != 0 || needs[0].Cache != 168*MiB || needs[0].Rootfs != 168*MiB {
		t.Fatalf("local OTA should not be counted again: %+v", needs)
	}
}
func TestUnifiedSpacePlanInspectOnly(t *testing.T) {
	needs, budget, e := composeSpacePlan("/cache", "/unused", "/tmp", fakeDisk(1), fakeDisk(1), fakeDisk(1), 0, 0, true)
	if e != nil {
		t.Fatal(e)
	}
	if len(needs) != 1 || needs[0].Rootfs != 0 || needs[0].Cache != 168*MiB || needs[0].Reserve != 64*MiB || budget.Need.Rootfs != 0 {
		t.Fatalf("inspect reserved installation: %+v %+v", needs, budget.Need)
	}
}
func TestContentLengthPreflightReportsFullSplit(t *testing.T) {
	dir := t.TempDir()
	var output bytes.Buffer
	meter := &progressMeter{Out: &output, Done: make(chan struct{})}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "16")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("0123456789abcdef"))
	}))
	defer server.Close()
	budget := downloadBudget{Need: diskNeed{Path: dir, Cache: 168 * MiB, Rootfs: 168 * MiB, Backup: 20 * MiB, Reserve: 64 * MiB}}
	name, cleanup, e := downloadWithBudget(context.Background(), server.URL+"/update.bin", dir, budget, meter)
	meter.Close()
	if e != nil {
		t.Fatal(e)
	}
	defer cleanup()
	if _, e = os.Stat(name); e != nil {
		t.Fatal(e)
	}
	if !strings.Contains(output.String(), "OTA download (HTTP Content-Length)") || !strings.Contains(output.String(), "extraction cache 168, staged runtime 168, backup 20, safety reserve 64 MiB") {
		t.Fatalf("content-length summary lacks categories: %s", output.String())
	}
	if !strings.Contains(output.String(), "OTA transfer complete") {
		t.Fatal(output.String())
	}
}
func TestPreflightWarnsThatOTAIsNotIncludedYet(t *testing.T) {
	dir := t.TempDir()
	var out bytes.Buffer
	meter := &progressMeter{Out: &out, Done: make(chan struct{})}
	budget, e := preflightCacheAndOTA(dir, dir, "inspect", true, false, meter)
	meter.Close()
	if e != nil {
		t.Fatal(e)
	}
	if budget.Need.Cache != 168*MiB || budget.Need.Rootfs != 0 {
		t.Fatalf("wrong inspect-only budget: %+v", budget.Need)
	}
	log := out.String()
	if !strings.Contains(log, "OTA size unknown until HTTP Content-Length") || !strings.Contains(log, "OTA 0, extraction cache 168, staged runtime 0, backup 0, safety reserve 64 MiB") {
		t.Fatalf("misleading preliminary capacity log: %s", log)
	}
}

func TestElapsedText(t *testing.T) {
	for _, tc := range []struct {
		d    time.Duration
		want string
	}{
		{234 * time.Millisecond, "234ms"},
		{1576 * time.Millisecond, "1.6s"},
		{4*time.Minute + 12*time.Second + 340*time.Millisecond, "4m12.3s"},
		{500 * time.Microsecond, "<1ms"},
	} {
		if got := elapsedText(tc.d); got != tc.want {
			t.Errorf("elapsed %s: got %s want %s", tc.d, got, tc.want)
		}
	}
}
