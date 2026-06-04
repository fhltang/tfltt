package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"tfltt/tfl/models"
)

func TestRenderTimetableTable(t *testing.T) {
	testCases := []struct {
		name     string
		dataFile string
	}{
		{
			name:     "Richmond (District)",
			dataFile: "testdata/richmond_district_timetable.json",
		},
		{
			name:     "Amersham (Metropolitan)",
			dataFile: "testdata/amersham_metropolitan_timetable.json",
		},
		{
			name:     "Rickmansworth (Metropolitan)",
			dataFile: "testdata/rickmansworth_metropolitan_timetable.json",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := os.ReadFile(tc.dataFile)
			if err != nil {
				t.Fatalf("Failed to read test data %s: %v", tc.dataFile, err)
			}

			var timetable models.TflAPIPresentationEntitiesTimetableResponse
			if err := json.Unmarshal(data, &timetable); err != nil {
				t.Fatalf("Failed to unmarshal test data: %v", err)
			}

			if len(timetable.Timetable.Routes) == 0 || len(timetable.Timetable.Routes[0].Schedules) == 0 {
				t.Fatalf("Test data missing routes or schedules")
			}
			route := timetable.Timetable.Routes[0]
			schedule := route.Schedules[0]

			renderer, err := NewTimetableRenderer(&timetable, route, schedule)
			if err != nil {
				t.Fatalf("Failed to create renderer: %v", err)
			}
			output := renderer.RenderAsText(20, 35)
			fmt.Printf("--- Table Test Output Start (%s) ---\n", tc.name)
			fmt.Println(output)
			fmt.Printf("--- Table Test Output End (%s) ---\n", tc.name)

			if len(output) < 100 {
				t.Errorf("Output too short, likely failed to render properly")
			}

			lines := strings.Split(strings.TrimRight(output, "\n"), "\n")
			hasCompressed := strings.Contains(output, "minutes past every hour")

			// Find separator and annotation row indices
			sepIdx := -1
			for i, l := range lines {
				if strings.HasPrefix(l, "---") {
					sepIdx = i
					break
				}
			}
			annotationIdx := -1
			for i, l := range lines {
				if strings.Contains(l, "minutes past every hour") {
					annotationIdx = i
					break
				}
			}

			// colHeaderIdx is the line immediately above the separator
			colHeaderIdx := -1
			if sepIdx > 0 {
				colHeaderIdx = sepIdx - 1
			}

			// isAnnotation returns true for the annotation row and the separator
			isAnnotation := func(idx int) bool {
				return idx == annotationIdx || idx == sepIdx
			}

			// 1. Pipe-position consistency: all non-annotation, non-separator lines after the
			// first metadata lines must have pipes at identical positions.
			if sepIdx >= 0 && colHeaderIdx >= 0 {
				pipePositions := func(line string) []int {
					var pos []int
					for i := 0; i+2 < len(line); i++ {
						if line[i] == ' ' && line[i+1] == '|' && line[i+2] == ' ' {
							pos = append(pos, i)
						}
					}
					return pos
				}
				refPositions := pipePositions(lines[colHeaderIdx])
				for i, l := range lines {
					if i <= colHeaderIdx || isAnnotation(i) || strings.TrimSpace(l) == "" {
						continue
					}
					got := pipePositions(l)
					if len(got) != len(refPositions) {
						t.Errorf("line %d: pipe count %d, want %d\n  %s", i, len(got), len(refPositions), l)
						continue
					}
					for k, pos := range refPositions {
						if got[k] != pos {
							t.Errorf("line %d: pipe %d at position %d, want %d\n  %s", i, k, got[k], pos, l)
						}
					}
				}
			}

			// 2. Column count per row
			if colHeaderIdx >= 0 {
				countPipes := func(line string) int {
					return strings.Count(line, " | ")
				}
				headerPipes := countPipes(lines[colHeaderIdx])
				for i, l := range lines[sepIdx+1:] {
					if strings.TrimSpace(l) == "" {
						continue
					}
					if got := countPipes(l); got != headerPipes {
						t.Errorf("data row %d: %d pipes, want %d\n  %s", i, got, headerPipes, l)
					}
				}
			}

			// 3. Annotation row position and isolation
			if hasCompressed {
				if annotationIdx < 0 {
					t.Errorf("expected annotation row with 'minutes past every hour'")
				} else {
					if annotationIdx >= colHeaderIdx {
						t.Errorf("annotation row (%d) must appear before column-label header (%d)", annotationIdx, colHeaderIdx)
					}
					if !strings.Contains(lines[annotationIdx], "until") {
						t.Errorf("annotation row missing 'until'")
					}
				}
				for i, l := range lines[sepIdx+1:] {
					if strings.Contains(l, "minutes past every hour") || strings.Contains(l, "until") {
						t.Errorf("data row %d contains annotation text: %s", i, l)
					}
				}
			}

			// 4. Minute cells under correct header columns
			if hasCompressed && colHeaderIdx >= 0 {
				// Find column indices (by pipe position) that hold bare minute labels
				headerCols := strings.Split(lines[colHeaderIdx], " | ")
				var minuteColIndices []int
				for ci, c := range headerCols {
					c = strings.TrimSpace(c)
					if len(c) <= 2 && len(c) >= 1 && c[0] >= '0' && c[0] <= '5' {
						minuteColIndices = append(minuteColIndices, ci)
					}
				}
				if len(minuteColIndices) == 0 {
					t.Errorf("expected minute column labels (e.g. '22', '52') in column-label header")
				}
				for _, l := range lines[sepIdx+1:] {
					if strings.TrimSpace(l) == "" {
						continue
					}
					cols := strings.Split(l, " | ")
					for _, ci := range minuteColIndices {
						if ci >= len(cols) {
							continue
						}
						cell := strings.TrimSpace(cols[ci])
						// Must be a two-digit number or "---", not HH:MM or blank
						isHHMM := len(cell) == 5 && cell[2] == ':'
						if isHHMM || cell == "" {
							t.Errorf("expected minute cell at col %d, got %q in: %s", ci, cell, l)
						}
					}
				}
			}

			// Verify HTML placeholder
			htmlOutput := renderer.RenderAsHtml(20)
			if !strings.Contains(htmlOutput, "<html>") {
				t.Errorf("HTML output doesn't look like HTML")
			}
		})
	}
}
