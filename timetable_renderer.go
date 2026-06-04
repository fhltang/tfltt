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

type compressedBlock struct {
	minuteJourneys []minuteJourney
	firstHour      int
	lastHour       int
}

type minuteJourney struct {
	minute     int
	intervalID int32
}

type TimetableRenderer struct {
	timetable    *models.TflAPIPresentationEntitiesTimetableResponse
	targetRoute  *models.TflAPIPresentationEntitiesTimetableRoute
	schedule     *models.TflAPIPresentationEntitiesSchedule
	stationNames map[string]string
	stops        []stopInfo
	intervalData map[int32]map[string]float64
}

func NewTimetableRenderer(timetableResponse *models.TflAPIPresentationEntitiesTimetableResponse, targetRoute *models.TflAPIPresentationEntitiesTimetableRoute, schedule *models.TflAPIPresentationEntitiesSchedule) (*TimetableRenderer, error) {
	if timetableResponse.Timetable == nil {
		return nil, fmt.Errorf("no timetable data available")
	}

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

func detectCompressedBlock(journeys []*models.TflAPIPresentationEntitiesKnownJourney) (prefix, suffixStart int, block *compressedBlock) {
	if len(journeys) == 0 {
		return 0, 0, nil
	}

	// Work on a sorted copy (by hour then minute) without mutating the caller's slice.
	sorted := make([]*models.TflAPIPresentationEntitiesKnownJourney, len(journeys))
	copy(sorted, journeys)
	// insertion sort — journey lists are small and usually already sorted
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			ah, am := 0, 0
			bh, bm := 0, 0
			fmt.Sscanf(sorted[j-1].Hour, "%d", &ah)
			fmt.Sscanf(sorted[j-1].Minute, "%d", &am)
			fmt.Sscanf(sorted[j].Hour, "%d", &bh)
			fmt.Sscanf(sorted[j].Minute, "%d", &bm)
			if ah*60+am > bh*60+bm {
				sorted[j-1], sorted[j] = sorted[j], sorted[j-1]
			} else {
				break
			}
		}
	}

	// Group journeys by hour.
	type hourGroup struct {
		hour     int
		startIdx int // index into sorted
		entries  []minuteJourney
	}
	var groups []hourGroup
	for i, j := 0, 0; i < len(sorted); i = j {
		h := 0
		fmt.Sscanf(sorted[i].Hour, "%d", &h)
		var entries []minuteJourney
		for j = i; j < len(sorted); j++ {
			gh := 0
			fmt.Sscanf(sorted[j].Hour, "%d", &gh)
			if gh != h {
				break
			}
			m := 0
			fmt.Sscanf(sorted[j].Minute, "%d", &m)
			entries = append(entries, minuteJourney{minute: m, intervalID: sorted[j].IntervalID})
		}
		groups = append(groups, hourGroup{hour: h, startIdx: i, entries: entries})
	}

	// minuteKey returns a comparable string for a group's minute pattern.
	minuteKey := func(g hourGroup) string {
		var b strings.Builder
		for _, e := range g.entries {
			fmt.Fprintf(&b, "%d,", e.minute)
		}
		return b.String()
	}

	// Find the longest run of compatible (consecutive hours + same minute pattern) groups.
	bestStart, bestLen := 0, 0
	for i := 0; i < len(groups); i++ {
		runLen := 1
		for j := i + 1; j < len(groups); j++ {
			if groups[j].hour == groups[j-1].hour+1 && minuteKey(groups[j]) == minuteKey(groups[i]) {
				runLen++
			} else {
				break
			}
		}
		if runLen > bestLen {
			bestLen = runLen
			bestStart = i
		}
	}

	if bestLen < 2 {
		return len(journeys), len(journeys), nil
	}

	bestEnd := bestStart + bestLen - 1 // inclusive group index
	prefix = groups[bestStart].startIdx
	if bestEnd+1 < len(groups) {
		suffixStart = groups[bestEnd+1].startIdx
	} else {
		suffixStart = len(sorted)
	}

	// Map sorted indices back to original slice indices by matching Hour+Minute+IntervalID.
	origIndex := func(s *models.TflAPIPresentationEntitiesKnownJourney) int {
		for k, j := range journeys {
			if j.Hour == s.Hour && j.Minute == s.Minute && j.IntervalID == s.IntervalID {
				return k
			}
		}
		return -1
	}
	prefix = origIndex(sorted[prefix])
	if suffixStart < len(sorted) {
		suffixStart = origIndex(sorted[suffixStart])
	} else {
		suffixStart = len(journeys)
	}

	block = &compressedBlock{
		minuteJourneys: groups[bestStart].entries,
		firstHour:      groups[bestStart].hour,
		lastHour:       groups[bestEnd].hour,
	}
	return prefix, suffixStart, block
}

func (tr *TimetableRenderer) RenderAsText(maxJourneys int, stationColWidth int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Timetable for %s at %s\n\n", tr.timetable.LineName, tr.timetable.Timetable.DepartureStopID)
	fmt.Fprintf(&sb, "Schedule: %s\n", tr.schedule.Name)

	journeys := tr.schedule.KnownJourneys
	if maxJourneys > 0 && len(journeys) > maxJourneys {
		journeys = journeys[:maxJourneys]
	}

	const colWidth = 10
	const compColWidth = 4

	prefix, suffixStart, block := detectCompressedBlock(journeys)
	prefixJourneys := journeys[:prefix]
	suffixJourneys := journeys[suffixStart:]

	// Annotation row (separate line above column headers, only when compressed block exists)
	if block != nil {
		prefixWidth := stationColWidth + len(prefixJourneys)*(colWidth+3)
		compWidth := len(block.minuteJourneys) * (compColWidth + 3)
		lastJ := journeys[suffixStart-1]
		lh, lm := 0, 0
		fmt.Sscanf(lastJ.Hour, "%d", &lh)
		fmt.Sscanf(lastJ.Minute, "%d", &lm)
		untilLabel := fmt.Sprintf("until %02d:%02d", lh, lm)
		fmt.Fprintf(&sb, "%-*s", prefixWidth, "")
		fmt.Fprintf(&sb, "%-*s", compWidth, "then at these minutes past every hour")
		fmt.Fprintf(&sb, " | %-*s", colWidth, untilLabel)
		fmt.Fprint(&sb, "\n")
	}

	// Column-label header row
	fmt.Fprintf(&sb, "%-*s", stationColWidth, "Station")
	for i := range prefixJourneys {
		fmt.Fprintf(&sb, " | %-*s", colWidth, fmt.Sprintf("Train %d", i+1))
	}
	if block != nil {
		for _, mj := range block.minuteJourneys {
			fmt.Fprintf(&sb, " | %-*d", compColWidth, mj.minute)
		}
	}
	suffixOffset := prefix
	if block != nil {
		suffixOffset = prefix + len(block.minuteJourneys)
	}
	for i := range suffixJourneys {
		fmt.Fprintf(&sb, " | %-*s", colWidth, fmt.Sprintf("Train %d", suffixOffset+i+1))
	}
	fmt.Fprint(&sb, "\n")

	// Separator
	totalWidth := stationColWidth + len(prefixJourneys)*(colWidth+3)
	if block != nil {
		totalWidth += len(block.minuteJourneys) * (compColWidth + 3)
	}
	totalWidth += len(suffixJourneys) * (colWidth + 3)
	fmt.Fprint(&sb, strings.Repeat("-", totalWidth))
	fmt.Fprint(&sb, "\n")

	// Data rows — single outer loop so each stop produces exactly one line
	for _, s := range tr.stops {
		name := s.name
		if len(name) > stationColWidth {
			name = name[:stationColWidth-3] + "..."
		}
		fmt.Fprintf(&sb, "%-*s", stationColWidth, name)

		writeJourneyCells := func(journeys []*models.TflAPIPresentationEntitiesKnownJourney, cw int) {
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
						fmt.Fprintf(&sb, " | %-*s", cw, calculateArrivalTime(j.Hour, j.Minute, off))
					} else {
						fmt.Fprintf(&sb, " | %-*s", cw, "---")
					}
				} else {
					fmt.Fprintf(&sb, " | %-*s", cw, "err")
				}
			}
		}

		writeJourneyCells(prefixJourneys, colWidth)

		if block != nil {
			for _, mj := range block.minuteJourneys {
				offsets, ok := tr.intervalData[mj.intervalID]
				if !ok {
					if len(tr.targetRoute.StationIntervals) > 0 {
						id64, _ := strconv.ParseInt(tr.targetRoute.StationIntervals[0].ID, 10, 32)
						offsets = tr.intervalData[int32(id64)]
						ok = true
					}
				}
				if ok {
					hour := fmt.Sprintf("%d", block.firstHour)
					minute := fmt.Sprintf("%d", mj.minute)
					off := offsets[s.id]
					fmt.Fprintf(&sb, " | %-*d", compColWidth, arrivalMinute(hour, minute, off))
				} else {
					fmt.Fprintf(&sb, " | %-*s", compColWidth, "err")
				}
			}
		}

		writeJourneyCells(suffixJourneys, colWidth)

		sb.WriteString("\n")
	}

	return sb.String()
}

func (tr *TimetableRenderer) RenderAsHtml(maxJourneys int) string {
	// TODO: Implement HTML rendering
	return "<html><body>HTML rendering not implemented yet</body></html>"
}

func arrivalMinute(hour, minute string, offsetMinutes float64) int {
	h, m := 0, 0
	fmt.Sscanf(hour, "%d", &h)
	fmt.Sscanf(minute, "%d", &m)
	return (h*60 + m + int(offsetMinutes)) % 60
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
