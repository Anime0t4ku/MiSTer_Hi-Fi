package main

type playerStatusLine struct {
	Text     string
	Scale, Y int
}

// Fit both loading lines above the progress bar, including narrow panels.
func playerStatusLines(title, detail string, width, height, preferred int) []playerStatusLine {
	if width <= 0 || height < 7 {
		return nil
	}
	fit := func(text string, scale int) int {
		for scale > 1 && tw(scale, text) > width {
			scale--
		}
		return max(1, scale)
	}
	titleScale := fit(title, max(1, preferred))
	detailScale := fit(detail, max(1, preferred-1))
	gap := 2
	if detail != "" {
		for 7*titleScale+gap+7*detailScale > height && (titleScale > 1 || detailScale > 1) {
			if titleScale >= detailScale && titleScale > 1 {
				titleScale--
			} else if detailScale > 1 {
				detailScale--
			}
		}
	} else {
		for titleScale > 1 && 7*titleScale > height {
			titleScale--
		}
	}
	lines := []playerStatusLine{{Text: short(title, max(1, width/(6*titleScale))), Scale: titleScale}}
	if detail != "" && 7*titleScale+gap+7*detailScale <= height {
		lines = append(lines, playerStatusLine{Text: short(detail, max(1, width/(6*detailScale))), Scale: detailScale, Y: 7*titleScale + gap})
	}
	return lines
}
