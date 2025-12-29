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

			// Verify HTML placeholder
			htmlOutput := renderer.RenderAsHtml(20)
			if !strings.Contains(htmlOutput, "<html>") {
				t.Errorf("HTML output doesn't look like HTML")
			}
		})
	}
}
