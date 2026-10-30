package ui

import (
	"testing"
	"time"

	"samouly.fun/twig/internal/git"
	"samouly.fun/twig/internal/watch"
)

// TestWatchDisabledKeepsNoWatcher covers the config switch: nothing is started
// and any running watcher is closed.
func TestWatchDisabledKeepsNoWatcher(t *testing.T) {
	m := testModel(120, 30)
	m.cfg.Watch = false

	if cmd := m.watchCommands([]git.FileStatus{{Path: "a.go", Letter: "M"}}, nil); cmd != nil {
		t.Fatal("a watcher command was issued while watching is off")
	}
	if m.watcher != nil {
		t.Fatal("a watcher was installed while watching is off")
	}
	if m.statusTick() != nil {
		t.Fatal("the poll chain kept running while watching is off")
	}
}

// TestRequestStatusIsSingleFlight covers the guard that keeps refreshes from
// stacking: a second request while one is in flight is dropped, and the next
// one after the result is allowed again.
func TestRequestStatusIsSingleFlight(t *testing.T) {
	m := testModel(120, 30)

	first := m.requestStatus(false)
	if first == nil {
		t.Fatal("the first request should schedule a status run")
	}
	if !m.statusPending {
		t.Fatal("the run was not marked in flight")
	}
	if second := m.requestStatus(false); second != nil {
		t.Fatal("a second request was queued behind the first")
	}

	next, _ := m.Update(statusLoadedMsg{fingerprint: "f"})
	after := next.(model)
	if after.statusPending {
		t.Fatal("the result did not clear the in-flight mark")
	}
	if again := after.requestStatus(false); again == nil {
		t.Fatal("no request was allowed after the previous result landed")
	}
}

// TestRefreshModeOnlyWhenDegraded covers the footer pill: nothing while the
// watcher is live or still starting, "polling" once events are not carrying
// the view.
func TestRefreshModeOnlyWhenDegraded(t *testing.T) {
	m := testModel(120, 30)
	if got := m.refreshMode(); got != "" {
		t.Fatalf("refresh mode = %q while the watcher is starting", got)
	}

	m.watchOff = true
	if got := m.refreshMode(); got != "polling" {
		t.Fatalf("refresh mode = %q without a backend, want polling", got)
	}
	m.watchOff = false

	w, err := watch.New(watch.Options{Dirs: []string{t.TempDir()}})
	if err != nil {
		t.Skipf("no watcher on this platform: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	m.watcher = w
	m.watchHealth = w.Health()
	if got := m.refreshMode(); got != "" {
		t.Fatalf("refresh mode = %q with a live watcher", got)
	}

	// a partial set is not live, and has to say so
	m.watchHealth.Failed = 1
	if got := m.refreshMode(); got != "polling" {
		t.Fatalf("refresh mode = %q with a failed watch, want polling", got)
	}
}

// TestPollMissDegradesTheWatcher covers a watcher that is installed but never
// delivers: the poll finding a change it did not report is the only symptom.
func TestPollMissDegradesTheWatcher(t *testing.T) {
	fresh := testModel(120, 30)
	fresh.watchHealth = watch.Health{Backend: "fake", Dirs: 1}
	fresh.watchLastEvent = time.Now()
	if fresh.checkWatchTrust(true, true) {
		t.Fatal("a recent event should not degrade the watcher")
	}

	manual := testModel(120, 30)
	manual.watchHealth = watch.Health{Backend: "fake", Dirs: 1}
	manual.watchLastEvent = time.Now().Add(-time.Minute)
	if manual.checkWatchTrust(false, true) {
		t.Fatal("a run the poll did not ask for should not degrade the watcher")
	}
	if manual.checkWatchTrust(true, false) {
		t.Fatal("a poll that found nothing should not degrade the watcher")
	}

	m := testModel(120, 30)
	m.watchHealth = watch.Health{Backend: "fake", Dirs: 1}
	m.watchLastEvent = time.Now().Add(-time.Minute)
	if !m.checkWatchTrust(true, true) {
		t.Fatal("a poll that found an unreported change should degrade the watcher")
	}
	if got := m.refreshMode(); got != "polling" {
		t.Fatalf("refresh mode = %q after degrading, want polling", got)
	}
	if m.checkWatchTrust(true, true) {
		t.Fatal("degrading twice should not report twice")
	}
	if m.watching() {
		t.Fatal("a degraded watcher must not count as watching")
	}
}
