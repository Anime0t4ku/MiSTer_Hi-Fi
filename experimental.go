package main

import "time"

func normalizeFPSTarget(fps int) int {
	if fps == 60 {
		return 60
	}
	return 30
}
func fpsInterval(fps int) time.Duration { return time.Second / time.Duration(normalizeFPSTarget(fps)) }
func fpsTargetLabel(fps int) string {
	if normalizeFPSTarget(fps) == 60 {
		return "60 FPS (Experimental)"
	}
	return "30 FPS (Default)"
}
