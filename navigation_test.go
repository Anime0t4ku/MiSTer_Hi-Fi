package main

import (
	"encoding/json"
	"testing"
	"time"
)

func TestListJumpBoundaries(t *testing.T) {
	items := []string{"A", "B", "", "C", "D"}
	cases := []struct {
		sel, delta int
		wrap       bool
		want       int
	}{
		{0, -5, true, 4}, {4, 5, true, 0}, {1, 2, true, 3},
		{0, -50, false, 0}, {4, 50, false, 4}, {1, 1, true, 3},
		{3, -1, true, 1}, {0, -500, true, 4}, {4, 500, true, 0},
	}
	for _, c := range cases {
		if got := moveListSelection(items, c.sel, c.delta, c.wrap); got != c.want {
			t.Errorf("%+v: got %d", c, got)
		}
	}
	if got := moveListSelection(nil, 0, 5, true); got != 0 {
		t.Fatal(got)
	}
	if got := moveListSelection([]string{""}, 0, 5, true); got != 0 {
		t.Fatal(got)
	}
}
func TestNavigationConfigMigration(t *testing.T) {
	n := defaultNavigationConfig()
	if err := json.Unmarshal([]byte(`{"jump_entries":50}`), &n); err != nil {
		t.Fatal(err)
	}
	if n.Jump != 50 || n.PageJump != 10 || !n.Wrap || !n.HoldRepeat || n.DelayMS != 400 || n.IntervalMS != 150 {
		t.Fatal(n)
	}
	n = NavigationConfig{Jump: -1, PageJump: 99999, DelayMS: 0, IntervalMS: 1}
	n.normalize()
	if n.Jump != 5 || n.PageJump != 10000 || n.DelayMS != 400 || n.IntervalMS != 150 {
		t.Fatal(n)
	}
}
func TestNavigationRepeatReleaseAndScope(t *testing.T) {
	configureNavigation(NavigationConfig{Jump: 5, PageJump: 10, HoldRepeat: true, DelayMS: 100, IntervalMS: 50})
	setNavigationActive(true)
	defer setNavigationActive(false)
	ch := make(chan action, 10)
	done := make(chan struct{})
	r := newNavigationRepeater(ch, done)
	defer r.close()
	r.update(inputEvent{Type: evKey, Code: keyLeft, Value: 1}, actLeft, false)
	select {
	case <-ch:
		t.Fatal("repeated before delay")
	case <-time.After(60 * time.Millisecond):
	}
	var queued action
	select {
	case queued = <-ch:
	case <-time.After(time.Second):
		t.Fatal("no repeat")
	}
	if a, ok := decodeNavigationRepeat(queued); !ok || a != actLeft {
		t.Fatal(a, ok)
	}
	r.update(inputEvent{Type: evKey, Code: keyLeft, Value: 0}, actNone, true)
	select {
	case <-ch:
		t.Fatal("repeated after release")
	case <-time.After(100 * time.Millisecond):
	}
	r.update(inputEvent{Type: evAbs, Code: 16, Value: 1}, actRight, false)
	r.update(inputEvent{Type: evAbs, Code: 16, Value: 1}, actNone, false)
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("unchanged axis event cancelled hold")
	}
	setNavigationActive(false)
	if _, ok := decodeNavigationRepeat(queued); ok {
		t.Fatal("repeat accepted outside browser")
	}
	setNavigationActive(true)
	if _, ok := decodeNavigationRepeat(queued); ok {
		t.Fatal("stale repeat accepted in new browser")
	}
	select {
	case <-ch:
		t.Fatal("hold carried across screens")
	case <-time.After(100 * time.Millisecond):
	}
	configureNavigation(NavigationConfig{HoldRepeat: false, DelayMS: 100, IntervalMS: 50})
	r.update(inputEvent{Type: evKey, Code: keyLeft, Value: 1}, actLeft, false)
	select {
	case <-ch:
		t.Fatal("repeat when disabled")
	case <-time.After(150 * time.Millisecond):
	}
}
