package main

import (
	"fmt"
	"strconv"
	"strings"
	"tfltt/tfl/models"
)

type stopInfo struct {
	id   string
	name string
}

type TimetableRenderer struct {
	timetable    *models.TflAPIPresentationEntitiesTimetableResponse
	targetRoute  *models.TflAPIPresentationEntitiesTimetableRoute
	schedule     *models.TflAPIPresentationEntitiesSchedule
	stationNames map[string]string
	stops        []stopInfo
	intervalData map[int32]map[string]float64
}

func NewTimetableRenderer(timetableResponse *models.TflAPIPresentationEntitiesTimetableResponse) (*TimetableRenderer, error) {
	if timetableResponse.Timetable == nil || len(timetableResponse.Timetable.Routes) == 0 {
		return nil, fmt.Errorf("no timetable data available")
	}

	// Find the first route with schedules
	var targetRoute *models.TflAPIPresentationEntitiesTimetableRoute
	for _, r := range timetableResponse.Timetable.Routes {
		if len(r.Schedules) > 0 {
			targetRoute = r
			break
		}
	}

	if targetRoute == nil {
		return nil, fmt.Errorf("no schedules found in any route")
	}

	// Use the first schedule
	schedule := targetRoute.Schedules[0]

	// Prepare name lookup map
	stationNames := make(map[string]string)
	for _, s := range timetableResponse.Stops {
		stationNames[s.ID] = s.Name
	}
	for _, s := range timetableResponse.Stations {
		if _, exists := stationNames[s.ID]; !exists {
			stationNames[s.ID] = s.Name + " [S]"
		}
	}

	var stops []stopInfo
	addedStops := make(map[string]bool)
	intervalData := make(map[int32]map[string]float64)

	depID := timetableResponse.Timetable.DepartureStopID
	stops = append(stops, stopInfo{id: depID, name: stationNames[depID]})
	addedStops[depID] = true

	// Build interval map and collect all unique stops in order
	for _, si := range targetRoute.StationIntervals {
		id64, _ := strconv.ParseInt(si.ID, 10, 32)
		idInt := int32(id64)

		m := make(map[string]float64)
		m[depID] = 0

		for _, intv := range si.Intervals {
			m[intv.StopID] = intv.TimeToArrival
			if !addedStops[intv.StopID] {
				addedStops[intv.StopID] = true
				stops = append(stops, stopInfo{id: intv.StopID, name: stationNames[intv.StopID]})
			}
		}
		intervalData[idInt] = m
	}

	return &TimetableRenderer{
		timetable:    timetableResponse,
		targetRoute:  targetRoute,
		schedule:     schedule,
		stationNames: stationNames,
		stops:        stops,
		intervalData: intervalData,
	}, nil
}

func (tr *TimetableRenderer) RenderAsText(maxJourneys int, stationColWidth int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Timetable for %s at %s\n\n", tr.timetable.LineName, tr.timetable.Timetable.DepartureStopID)
	fmt.Fprintf(&sb, "Schedule: %s\n", tr.schedule.Name)

	journeys := tr.schedule.KnownJourneys
	if maxJourneys > 0 && len(journeys) > maxJourneys {
		journeys = journeys[:maxJourneys]
	}

	// Header
	const colWidth = 10
	fmt.Fprintf(&sb, "%-*s", stationColWidth, "Station")
	for i := range journeys {
		fmt.Fprintf(&sb, " | %-*s", colWidth, fmt.Sprintf("Train %d", i+1))
	}
	fmt.Fprint(&sb, "\n")
	fmt.Fprint(&sb, strings.Repeat("-", stationColWidth+len(journeys)*(colWidth+3)))
	fmt.Fprint(&sb, "\n")

	// Rows
	for _, s := range tr.stops {
		name := s.name
		if len(name) > stationColWidth {
			name = name[:stationColWidth-3] + "..."
		}
		fmt.Fprintf(&sb, "%-*s", stationColWidth, name)

		for _, j := range journeys {
			offsets, ok := tr.intervalData[j.IntervalID]
			if !ok {
				if len(tr.targetRoute.StationIntervals) > 0 {
					id64, _ := strconv.ParseInt(tr.targetRoute.StationIntervals[0].ID, 10, 32)
					offsets = tr.intervalData[int32(id64)]
					ok = true
				}
			}

			if ok {
				off, found := offsets[s.id]
				if found {
					arrTime := calculateArrivalTime(j.Hour, j.Minute, off)
					fmt.Fprintf(&sb, " | %-*s", colWidth, arrTime)
				} else {
					fmt.Fprintf(&sb, " | %-*s", colWidth, "---")
				}
			} else {
				fmt.Fprintf(&sb, " | %-*s", colWidth, "err")
			}
		}
		sb.WriteString("\n")
	}

	return sb.String()
}

func (tr *TimetableRenderer) RenderAsHtml(maxJourneys int) string {
	// TODO: Implement HTML rendering
	return "<html><body>HTML rendering not implemented yet</body></html>"
}

func calculateArrivalTime(hour, minute string, offsetMinutes float64) string {
	h := 0
	fmt.Sscanf(hour, "%d", &h)
	m := 0
	fmt.Sscanf(minute, "%d", &m)

	totalMinutes := h*60 + m + int(offsetMinutes)
	newH := (totalMinutes / 60) % 24
	newM := totalMinutes % 60

	return fmt.Sprintf("%02d:%02d", newH, newM)
}
