package main

import "testing"

func TestRadioNameMatch(t *testing.T) {
	if !radioNameMatch("Black Keys", "The Black Keys") || !radioNameMatch("Lonely Boy", "Lonely Boy (Radio Edit)") {
		t.Fatal("expected match")
	}
	if radioNameMatch("KINK", "Metallica") || radioNameMatch("", "x") {
		t.Fatal("unexpected match")
	}
}
