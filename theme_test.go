package main

import "testing"

func TestAccentParseAndCycle(t *testing.T) {
	if got := setAccent("#ff9500"); got != "#FF9500" {
		t.Fatalf("got %s", got)
	}
	if got := setAccent("nonsense"); got != defaultAccentHex {
		t.Fatalf("invalid colour should fall back, got %s", got)
	}
	if got := cycleAccent("#F0F0F0", 1); got != "#FF3B30" {
		t.Fatalf("got %s", got)
	}
	if got := cycleAccent("#F0F0F0", -1); got != accentPresets[len(accentPresets)-1].Hex {
		t.Fatalf("got %s", got)
	}
	if got := accentLabel("#123456"); got != "CUSTOM #123456" {
		t.Fatalf("got %s", got)
	}
	setAccent(defaultAccentHex)
}

func TestVisualizerCycle(t *testing.T) {
	id := "bars"
	for i := 0; i < len(visualizerStyles); i++ {
		id = cycleVisualizer(id, 1)
	}
	if id != "bars" {
		t.Fatalf("cycle should wrap, got %s", id)
	}
}
