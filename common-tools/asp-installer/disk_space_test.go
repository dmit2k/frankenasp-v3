package main

import (
	"github.com/dmit2k/frankenasp-v3/internal/asp"
	"math"
	"strings"
	"testing"
)

func TestSpaceBudgetRejectsShortageBeforeWrite(t *testing.T) {
	need := diskNeed{Role: "test-filesystem", OTA: 100 << 20, Rootfs: 20 << 20, Backup: 10 << 20, Reserve: 64 << 20}
	exact := uint64(194) << 20
	report, e := evaluateSpace(exact, need)
	if e != nil || report.RequiredMiB != 194 {
		t.Fatalf("exact budget rejected: %+v %v", report, e)
	}
	report, e = evaluateSpace(exact-1, need)
	if e == nil || !strings.Contains(e.Error(), "insufficient disk space") {
		t.Fatalf("shortage accepted: %+v %v", report, e)
	}
	if report.AvailableMiB != 193 || report.ReserveMiB != 64 {
		t.Fatalf("incorrect budget report: %+v", report)
	}
}
func TestSpaceBudgetOverflow(t *testing.T) {
	if _, e := sumSpace(math.MaxUint64, 1); e == nil {
		t.Fatal("overflow permitted")
	}
	if _, e := evaluateSpace(math.MaxUint64, diskNeed{OTA: math.MaxUint64, Reserve: 1}); e == nil {
		t.Fatal("overflow accepted in budget")
	}
}
func TestActualRootfsBudget(t *testing.T) {
	items := map[string][]byte{"system/bin/linker": make([]byte, 5000), "vendor/lib/libasp.so": make([]byte, 6000)}
	plan := asp.AssetPlan{Platform: "athena", Files: map[string][]byte{
		"assets/external-asp": []byte("l"),
		"assets/asp1-child":   []byte("h"),
		"assets/channel-map":  []byte("cfg"),
	}, Installed: []asp.ReleaseFile{
		{Path: "assets/external-asp", Role: "launcher", InstallTo: "bin/external-asp"},
		{Path: "assets/asp1-child", Role: "child", InstallTo: "lib/rootfs/factory/asp1-child"},
		{Path: "assets/channel-map", Role: "data", InstallTo: "lib/rootfs/factory/channel-map"},
	}}
	full, e := predictedRootfsBytesPlan(items, plan, "bundled", 4096)
	if e != nil {
		t.Fatal(e)
	}
	live, e := predictedRootfsBytesPlan(items, plan, "live", 4096)
	if e != nil {
		t.Fatal(e)
	}
	if full-live != 12288 {
		t.Fatalf("live vendor was not subtracted: full=%d live=%d", full, live)
	}
	if _, e = predictedRootfsBytesPlan(items, plan, "bundled", 0); e == nil {
		t.Fatal("zero block accepted")
	}
	// The planned inventory, not external asset metadata, is authoritative.
	plan.Installed = append(plan.Installed, asp.ReleaseFile{Path: "assets/missing", Role: "data", InstallTo: "lib/rootfs/factory/missing"})
	if _, e = predictedRootfsBytesPlan(items, plan, "bundled", 4096); e == nil {
		t.Fatal("incomplete verified plan accepted")
	}
}
func TestLiveAndBundledSpaceRequirements(t *testing.T) {
	budget := diskNeed{Role: "update", Rootfs: 32 << 20, Backup: 20 << 20, Reserve: spaceReserve}
	_, e := evaluateSpace(120<<20, budget)
	if e != nil {
		t.Fatalf("backup update should fit: %v", e)
	}
	_, e = evaluateSpace(100<<20, budget)
	if e == nil {
		t.Fatal("backup update should not fit")
	}
	budget.Backup = 0 // --force-no-backup excludes archive but not staged rootfs.
	_, e = evaluateSpace(100<<20, budget)
	if e != nil {
		t.Fatalf("explicit no-backup should fit: %v", e)
	}
	budget.Rootfs = 0
	_, e = evaluateSpace(63<<20, budget)
	if e == nil {
		t.Fatal("reserve can never be waived")
	}
}
func TestStatfsUsable(t *testing.T) {
	snap, e := diskAt(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	if snap.Available == 0 || snap.BlockSize == 0 || snap.Dev == 0 {
		t.Fatalf("unexpected filesystem metadata %+v", snap)
	}
}
