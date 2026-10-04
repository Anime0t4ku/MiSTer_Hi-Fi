package main

import (
	"sync"
	"sync/atomic"
	"time"
)

type NavigationConfig struct {
	Jump       int  `json:"jump_entries"`
	PageJump   int  `json:"page_jump_entries"`
	Wrap       bool `json:"wrap"`
	HoldRepeat bool `json:"hold_repeat"`
	DelayMS    int  `json:"hold_delay_ms"`
	IntervalMS int  `json:"repeat_interval_ms"`
}

func defaultNavigationConfig() NavigationConfig { return NavigationConfig{5, 10, true, true, 400, 150} }
func (n *NavigationConfig) normalize() {
	if n.Jump < 1 {
		n.Jump = 5
	}
	if n.Jump > 10000 {
		n.Jump = 10000
	}
	if n.PageJump < 1 {
		n.PageJump = 10
	}
	if n.PageJump > 10000 {
		n.PageJump = 10000
	}
	if n.DelayMS < 100 {
		n.DelayMS = 400
	}
	if n.DelayMS > 5000 {
		n.DelayMS = 5000
	}
	if n.IntervalMS < 50 {
		n.IntervalMS = 150
	}
	if n.IntervalMS > 2000 {
		n.IntervalMS = 2000
	}
}
func moveListSelection(items []string, sel, delta int, wrap bool) int {
	if len(items) == 0 {
		return 0
	}
	target := sel + delta
	if wrap && target < 0 {
		target = len(items) - 1
	} else if wrap && target >= len(items) {
		target = 0
	}
	if target < 0 {
		target = 0
	}
	if target >= len(items) {
		target = len(items) - 1
	}
	dir := 1
	if delta < 0 {
		dir = -1
	}
	for i := 0; i < len(items); i++ {
		if items[target] != "" {
			return target
		}
		target += dir
		if target < 0 || target >= len(items) {
			if !wrap {
				return sel
			}
			target = (target + len(items)) % len(items)
		}
	}
	return sel
}

var navigationActive atomic.Bool
var navigationEpoch atomic.Int64
var navigationDelay atomic.Int64
var navigationInterval atomic.Int64
var navigationHold atomic.Bool

const repeatActionBase action = 1000

func setNavigationActive(active bool) {
	if navigationActive.Swap(active) != active {
		navigationEpoch.Add(1)
	}
}
func configureNavigation(n NavigationConfig) {
	n.normalize()
	navigationDelay.Store(int64(n.DelayMS))
	navigationInterval.Store(int64(n.IntervalMS))
	navigationHold.Store(n.HoldRepeat)
}
func decodeNavigationRepeat(a action) (action, bool) {
	v := int64(a - repeatActionBase)
	return action(v % 10), navigationActive.Load() && v/10 == navigationEpoch.Load()
}

type navigationRepeater struct {
	mu        sync.Mutex
	held      action
	code      uint16
	eventType uint16
	epoch     int64
	next      time.Time
	stop      chan struct{}
}

func newNavigationRepeater(ch chan<- action, done <-chan struct{}) *navigationRepeater {
	r := &navigationRepeater{stop: make(chan struct{})}
	go func() {
		tick := time.NewTicker(10 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-r.stop:
				return
			case now := <-tick.C:
				r.mu.Lock()
				if r.held != actNone && (!navigationActive.Load() || r.epoch != navigationEpoch.Load() || !navigationHold.Load()) {
					r.held = actNone
				}
				if r.held != actNone && !now.Before(r.next) {
					a := repeatActionBase + action(r.epoch*10) + r.held
					select {
					case ch <- a:
					default:
					}
					r.next = now.Add(time.Duration(navigationInterval.Load()) * time.Millisecond)
				}
				r.mu.Unlock()
			}
		}
	}()
	return r
}
func (r *navigationRepeater) close() { close(r.stop) }
func (r *navigationRepeater) update(ev inputEvent, a action, released bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ev.Type == r.eventType && ev.Code == r.code && released {
		r.held = actNone
	}
	if a != actLeft && a != actRight && a != actUp && a != actDown {
		return
	}
	if !navigationActive.Load() || !navigationHold.Load() || screenSaverActive.Load() {
		return
	}
	r.held = a
	r.code = ev.Code
	r.eventType = ev.Type
	r.epoch = navigationEpoch.Load()
	r.next = time.Now().Add(time.Duration(navigationDelay.Load()) * time.Millisecond)
}
func cycleNavigationValue(current, dir int, values []int) int {
	for i, v := range values {
		if v == current {
			return values[(i+dir+len(values))%len(values)]
		}
	}
	if dir < 0 {
		for i := len(values) - 1; i >= 0; i-- {
			if values[i] < current {
				return values[i]
			}
		}
		return values[len(values)-1]
	}
	for _, v := range values {
		if v > current {
			return v
		}
	}
	return values[0]
}
