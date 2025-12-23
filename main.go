package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"tfltt/tfl/client"
	"tfltt/tfl/client/line"
	"tfltt/tfl/client/stop_point"
	"tfltt/tfl/models"

	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
	"github.com/go-openapi/swag"
)

// AppKeyAuthWriter implements runtime.ClientAuthInfoWriter
type AppKeyAuthWriter struct {
	AppKey string
}

func (a *AppKeyAuthWriter) AuthenticateRequest(r runtime.ClientRequest, registry strfmt.Registry) error {
	return r.SetQueryParam("app_key", a.AppKey)
}

// UserAgentTransport authenticates requests and sets User-Agent
type UserAgentTransport struct {
	Transport http.RoundTripper
}

func (t *UserAgentTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("User-Agent", "TFL-Go-Client/1.0")
	return t.Transport.RoundTrip(req)
}

const (
	TargetStationName = "Richmond"
	Mode              = "tube"
)

type LineStopPair struct {
	LineID      string
	StopPointID string
}

func main() {
	// Read API key from file
	keyBytes, err := os.ReadFile("app_key.txt")
	if err != nil {
		log.Fatalf("Error reading app_key.txt: %v", err)
	}
	appKey := strings.TrimSpace(string(keyBytes))

	// Auth writer
	auth := &AppKeyAuthWriter{AppKey: appKey}

	// Create transport with custom User-Agent and Default Authentication
	cfg := client.DefaultTransportConfig().WithHost("api.tfl.gov.uk")
	transport := httptransport.New(cfg.Host, cfg.BasePath, cfg.Schemes)
	transport.Transport = &UserAgentTransport{Transport: http.DefaultTransport}
	transport.DefaultAuthentication = auth

	// Create client
	tflClient := client.New(transport, strfmt.Default)

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<h1>TFL Timetable Search</h1>")
		fmt.Fprint(w, "<form method='GET' action='/'>")
		fmt.Fprintf(w, "<input type='text' name='q' value='%s' placeholder='Enter station name...'>", q)
		fmt.Fprint(w, "<button type='submit'>Search</button>")
		fmt.Fprint(w, "</form>")

		if q != "" {
			pairs, err := getLinesAndStops(tflClient, q, "tube")
			if err != nil {
				fmt.Fprintf(w, "<p style='color:red'>Error: %v</p>", err)
				return
			}

			if len(pairs) == 0 {
				fmt.Fprintf(w, "<p>No results found for '%s'</p>", q)
				return
			}

			fmt.Fprintf(w, "<h2>Results for '%s'</h2>", q)
			fmt.Fprint(w, "<ul>")
			for _, pair := range pairs {
				timetableURL := fmt.Sprintf("/timetable?line_id=%s&stop_point_id=%s", pair.LineID, pair.StopPointID)
				fmt.Fprintf(w, "<li><a href='%s'>%s Line at Stop %s</a></li>", timetableURL, strings.Title(pair.LineID), pair.StopPointID)
			}
			fmt.Fprint(w, "</ul>")
		}
	})

	http.HandleFunc("/demo", DemoHandler(tflClient))
	http.HandleFunc("/timetable", TimetableHandler(tflClient))

	log.Println("Starting server on :8080...")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func DemoHandler(tflClient *client.Tfl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pairs, err := getLinesAndStops(tflClient, TargetStationName, Mode)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error getting lines and stops for %s: %v", TargetStationName, err), http.StatusInternalServerError)
			return
		}

		if len(pairs) == 0 {
			http.Error(w, fmt.Sprintf("No lines or stops found for %s", TargetStationName), http.StatusNotFound)
			return
		}

		// For demo purposes, we'll just use the first pair (e.g., District line at Richmond)
		pair := pairs[0]

		// Redirect to /timetable
		timetableURL := fmt.Sprintf("/timetable?line_id=%s&stop_point_id=%s", pair.LineID, pair.StopPointID)
		http.Redirect(w, r, timetableURL, http.StatusFound)
	}
}

func TimetableHandler(tflClient *client.Tfl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		lineID := r.URL.Query().Get("line_id")
		stopPointID := r.URL.Query().Get("stop_point_id")

		if lineID == "" || stopPointID == "" {
			http.Error(w, "Missing line_id or stop_point_id", http.StatusBadRequest)
			return
		}

		// 2. Get Line ID Timetable from target station
		timetableParams := line.NewLineTimetableParams()
		timetableParams.ID = lineID
		timetableParams.FromStopPointID = stopPointID

		timetableResp, err := tflClient.Line.LineTimetable(timetableParams)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error getting timetable: %v", err), http.StatusInternalServerError)
			return
		}

		payload := timetableResp.Payload
		// 3. Handle Disambiguation if needed
		if payload.Timetable == nil && payload.Disambiguation != nil && len(payload.Disambiguation.DisambiguationOptions) > 0 {
			option := payload.Disambiguation.DisambiguationOptions[0]
			u, err := url.Parse(option.URI)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error parsing disambiguation URI: %v", err), http.StatusInternalServerError)
				return
			}
			queryParams := u.Query()

			timetableResp, err = tflClient.Line.LineTimetable(timetableParams, func(op *runtime.ClientOperation) {
				originalWriter := op.Params
				op.Params = runtime.ClientRequestWriterFunc(func(r runtime.ClientRequest, reg strfmt.Registry) error {
					if err := originalWriter.WriteToRequest(r, reg); err != nil {
						return err
					}
					for k, vs := range queryParams {
						for _, v := range vs {
							if err := r.SetQueryParam(k, v); err != nil {
								return err
							}
						}
					}
					return nil
				})
			})
			if err != nil {
				http.Error(w, fmt.Sprintf("Error getting disambiguated timetable: %v", err), http.StatusInternalServerError)
				return
			}
			payload = timetableResp.Payload
		}

		if payload != nil {
			renderer, err := NewTimetableRenderer(payload)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error creating timetable renderer: %v", err), http.StatusInternalServerError)
				return
			}
			output := renderer.RenderAsText(200, 50)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<html><body><h1>Timetable for %s</h1><pre>%s</pre></body></html>", stopPointID, output)
		} else {
			http.Error(w, "No timetable payload received", http.StatusNoContent)
		}
	}
}

func getLinesAndStops(tflClient *client.Tfl, stationName string, mode string) ([]LineStopPair, error) {
	matches, err := searchStopPoints(tflClient, stationName, mode)
	if err != nil {
		return nil, err
	}

	if len(matches) == 0 {
		return nil, nil
	}

	// Collect IDs from matches
	ids := make([]string, 0, len(matches))
	for _, match := range matches {
		ids = append(ids, match.ID)
	}

	// Workaround: The TFL API returns a single object for a single ID, but the generated SDK
	// expects a JSON array. By requesting two distinct IDs, we force the API to return an array.
	if len(ids) == 1 {
		dummyID := "HUBAMR" // Amersham (different from Richmond's 940GZZLURMD/HUBRMD)
		if ids[0] == dummyID {
			dummyID = "HUBRMD" // Richmond
		}
		ids = append(ids, dummyID)
	}

	// Fetch all StopPoint details in one batch call
	params := stop_point.NewStopPointGetParams()
	params.Ids = ids
	resp, err := tflClient.StopPoint.StopPointGet(params)
	if err != nil {
		return nil, fmt.Errorf("error fetching stop points: %w", err)
	}

	var pairs []LineStopPair
	var naptanIDs []string

	for _, sp := range resp.Payload {
		if sp.ID == "HUBAMR" && stationName != "Amersham" {
			continue
		}

		if strings.HasPrefix(sp.ID, "HUB") {
			// If it's a hub, we must explore children for specific Naptans
			naptanIDs = append(naptanIDs, getSpecificIDsFromChildren(sp.Children)...)
			continue
		}

		naptanIDs = append(naptanIDs, sp.ID)
	}

	// Fetch the specific children
	if len(naptanIDs) > 0 {

		// Apply same workaround for single child ID
		if len(naptanIDs) == 1 {
			naptanIDs = append(naptanIDs, "HUBAMR")
		}

		params.Ids = naptanIDs
		resp, err = tflClient.StopPoint.StopPointGet(params)
		if err != nil {
			return nil, fmt.Errorf("error fetching child stop points: %w", err)
		}
		for _, sp := range resp.Payload {
			if sp.ID == "HUBAMR" && stationName != "Amersham" {
				continue
			}
			for _, l := range sp.Lines {
				pairs = append(pairs, LineStopPair{
					LineID:      l.ID,
					StopPointID: sp.NaptanID,
				})
			}
		}
	}

	return pairs, nil
}

func getSpecificIDsFromChildren(places []*models.TflAPIPresentationEntitiesPlace) []string {
	var ids []string
	for _, p := range places {
		// 940G is the prefix for Underground stations/stops
		if strings.HasPrefix(p.ID, "940G") {
			ids = append(ids, p.ID)
		}
		ids = append(ids, getSpecificIDsFromChildren(p.Children)...)
	}
	return ids
}

func searchStopPoints(tflClient *client.Tfl, targetStationName string, mode string) ([]*models.TflAPIPresentationEntitiesSearchMatch, error) {
	searchParams := stop_point.NewStopPointSearchParams()
	searchParams.Query = targetStationName
	searchParams.Modes = []string{mode}
	searchParams.IncludeHubs = swag.Bool(false)

	searchResp, err := tflClient.StopPoint.StopPointSearch(searchParams)
	if err != nil {
		return nil, err
	}

	return searchResp.Payload.Matches, nil
}

// resolveStopPointID ensures we use a specific Naptan ID instead of a Hub ID for timetables
func resolveStopPointID(tflClient *client.Tfl, id string, mode string) (string, error) {
	if !strings.HasPrefix(id, "HUB") {
		return id, nil
	}

	params := stop_point.NewStopPointGetParams()
	// Workaround: The TFL API returns a single object for a single ID, but the generated SDK
	// expects a JSON array. By requesting two distinct IDs, we force the API to return an array.
	dummyID := "HUBRMD" // Richmond
	if id == dummyID {
		dummyID = "HUBAMR" // Amersham
	}
	params.Ids = []string{id, dummyID}

	resp, err := tflClient.StopPoint.StopPointGet(params)
	if err != nil {
		return "", err
	}

	if len(resp.Payload) == 0 {
		return id, nil
	}

	// Find the stop point that matches our target ID (since order isn't guaranteed)
	var targetSP *models.TflAPIPresentationEntitiesStopPoint
	for _, sp := range resp.Payload {
		if strings.EqualFold(sp.ID, id) {
			targetSP = sp
			break
		}
	}

	if targetSP == nil {
		// Fallback to first item if match not found
		targetSP = resp.Payload[0]
	}

	if deepID := findDeepID(targetSP.Children); deepID != "" {
		return deepID, nil
	}

	return id, nil
}

func findDeepID(places []*models.TflAPIPresentationEntitiesPlace) string {
	for _, p := range places {
		if strings.HasPrefix(p.ID, "940G") {
			return p.ID
		}
		if id := findDeepID(p.Children); id != "" {
			return id
		}
	}
	return ""
}
