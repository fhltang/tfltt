package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"tfltt/tfl/client/stop_point"
	"tfltt/tfl/models"

	"github.com/go-openapi/runtime"
)

type MockStopPointClient struct {
	stop_point.ClientService
	SearchResponse *stop_point.StopPointSearchOK
	GetResponse    []*models.TflAPIPresentationEntitiesStopPoint
	SearchErr      error
	GetErr         error
}

func (m *MockStopPointClient) StopPointSearch(params *stop_point.StopPointSearchParams, opts ...stop_point.ClientOption) (*stop_point.StopPointSearchOK, error) {
	return m.SearchResponse, m.SearchErr
}

func (m *MockStopPointClient) StopPointGet(params *stop_point.StopPointGetParams, opts ...stop_point.ClientOption) (*stop_point.StopPointGetOK, error) {
	if m.GetErr != nil {
		return nil, m.GetErr
	}
	return &stop_point.StopPointGetOK{Payload: m.GetResponse}, nil
}

func (m *MockStopPointClient) SetTransport(transport runtime.ClientTransport) {}

func TestGetLinesAndStops(t *testing.T) {
	// Load Mock Data
	searchData, err := os.ReadFile("testdata/richmond_tube_stoppoint_search.json")
	if err != nil {
		t.Fatalf("failed to read search data: %v", err)
	}
	var searchPayload models.TflAPIPresentationEntitiesSearchResponse
	if err := json.Unmarshal(searchData, &searchPayload); err != nil {
		t.Fatalf("failed to unmarshal search data: %v", err)
	}

	getData, err := os.ReadFile("testdata/HUBRMD_stopppoint_get.json")
	if err != nil {
		t.Fatalf("failed to read get data: %v", err)
	}
	var rawData []any
	if err := json.Unmarshal(getData, &rawData); err != nil {
		t.Fatalf("failed to unmarshal get data: %v", err)
	}

	// Flatten the hierarchy so simplified StopPointGet returns all points at top level
	var getPayload []*models.TflAPIPresentationEntitiesStopPoint
	var flatten func([]any)
	flatten = func(items []any) {
		for _, item := range items {
			obj, ok := item.(map[string]any)
			if !ok {
				continue
			}
			// Convert individual map to StopPoint object
			data, _ := json.Marshal(obj)
			var sp models.TflAPIPresentationEntitiesStopPoint
			json.Unmarshal(data, &sp)
			getPayload = append(getPayload, &sp)

			// Recurse into children if they exist
			if children, ok := obj["children"].([]any); ok {
				flatten(children)
			}
		}
	}
	flatten(rawData)

	mockClient := &MockStopPointClient{
		SearchResponse: &stop_point.StopPointSearchOK{Payload: &searchPayload},
		GetResponse:    getPayload,
	}

	pairs, err := getLinesAndStops(mockClient, "Richmond", "tube")
	if err != nil {
		t.Fatalf("getLinesAndStops failed: %v", err)
	}

	if len(pairs) == 0 {
		t.Error("expected lines and stops, got none")
	}

	// Verify we got Richmond (District/Overground/etc)
	foundDistrictAtStation := false
	for _, p := range pairs {
		if p.LineID == "district" && p.StopPointID == "940GZZLURMD" {
			foundDistrictAtStation = true
			break
		}
	}

	if !foundDistrictAtStation {
		t.Error("did not find district line at station 940GZZLURMD in results")
	}
}

// Simplified mock doesn't need recursive population

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

			renderer, err := NewTimetableRenderer(&timetable)
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
