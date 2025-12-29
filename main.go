package main

import (
	"fmt"
	"log"
	"net/http"
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
	// Read API key from environment variable or file
	appKey := os.Getenv("TFL_APP_KEY")
	if appKey == "" {
		keyBytes, err := os.ReadFile("app_key.txt")
		if err != nil {
			log.Printf("Warning: TFL_APP_KEY not set and error reading app_key.txt: %v", err)
		} else {
			appKey = strings.TrimSpace(string(keyBytes))
		}
	}

	if appKey == "" {
		log.Println("Warning: No TfL API key found. API calls may fail.")
	}

	// Auth writer
	auth := &AppKeyAuthWriter{AppKey: appKey}

	// Create transport with custom User-Agent and Default Authentication
	cfg := client.DefaultTransportConfig().WithHost("api.tfl.gov.uk")
	transport := httptransport.New(cfg.Host, cfg.BasePath, cfg.Schemes)
	transport.Transport = &UserAgentTransport{Transport: http.DefaultTransport}
	transport.DefaultAuthentication = auth

	// Create client
	tflClient := client.New(transport, strfmt.Default)

	http.HandleFunc("/", DefaultHandler(tflClient.StopPoint))

	http.HandleFunc("/demo", DemoHandler(tflClient.StopPoint))
	http.HandleFunc("/timetable", TimetableHandler(tflClient))
	http.HandleFunc("/routes", RoutesHandler(tflClient))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on :%s...", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func DefaultHandler(stopPointClient stop_point.ClientService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query().Get("q")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<h1>TFL Timetable Search</h1>")
		fmt.Fprint(w, "<form method='GET' action='/'>")
		fmt.Fprintf(w, "<input type='text' name='q' value='%s' placeholder='Enter station name...'>", q)
		fmt.Fprint(w, "<button type='submit'>Search</button>")
		fmt.Fprint(w, " <a href='/?q=Richmond'>Demo</a>")
		fmt.Fprint(w, " <a href='/routes'>Routes</a>")
		fmt.Fprint(w, "</form>")

		if q != "" {
			pairs, err := getLinesAndStops(stopPointClient, q, "tube")
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
	}
}

func DemoHandler(stopPointClient stop_point.ClientService) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pairs, err := getLinesAndStops(stopPointClient, TargetStationName, Mode)
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
		lineID := r.URL.Query().Get("line")
		fromID := r.URL.Query().Get("from")
		toID := r.URL.Query().Get("to")

		if lineID == "" || fromID == "" || toID == "" {
			http.Error(w, "Missing required parameters: line, from, to", http.StatusBadRequest)
			return
		}

		params := line.NewLineTimetableToParams()
		params.ID = lineID
		params.FromStopPointID = fromID
		params.ToStopPointID = toID

		timetableResp, err := tflClient.Line.LineTimetableTo(params)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error getting timetable: %v", err), http.StatusInternalServerError)
			return
		}

		payload := timetableResp.Payload
		if payload != nil {
			renderer, err := NewTimetableRenderer(payload)
			if err != nil {
				http.Error(w, fmt.Sprintf("Error creating timetable renderer: %v", err), http.StatusInternalServerError)
				return
			}
			output := renderer.RenderAsText(200, 50)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<html><body><h1>Timetable for %s from %s to %s</h1><pre>%s</pre></body></html>", lineID, fromID, toID, output)
		} else {
			http.Error(w, "No timetable payload received", http.StatusNoContent)
		}
	}
}

func getLinesAndStops(stopPointClient stop_point.ClientService, stationName string, mode string) ([]LineStopPair, error) {
	searchParams := stop_point.NewStopPointSearchParams()
	searchParams.Query = stationName
	searchParams.Modes = []string{mode}
	searchParams.IncludeHubs = swag.Bool(false)

	searchResp, err := stopPointClient.StopPointSearch(searchParams)
	if err != nil {
		return nil, err
	}

	matches := searchResp.Payload.Matches
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
	resp, err := stopPointClient.StopPointGet(params)
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
		resp, err = stopPointClient.StopPointGet(params)
		if err != nil {
			return nil, fmt.Errorf("error fetching child stop points: %w", err)
		}
		for _, sp := range resp.Payload {
			if strings.HasPrefix(sp.ID, "HUB") && (stationName != "Amersham" || sp.ID != "HUBAMR") {
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

func RoutesHandler(tflClient *client.Tfl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := line.NewLineRouteByModeParams()
		params.Modes = []string{"tube"}

		resp, err := tflClient.Line.LineRouteByMode(params)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching routes: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><body><h1>Tube Lines and Routes</h1><ul>")

		for _, l := range resp.Payload {
			fmt.Fprintf(w, "<li>%s<ul>", l.Name)
			for _, route := range l.RouteSections {
				// Construct link to timetable
				// /timetable?line={lineID}&from={originationName}&to={destinationName}
				// Note: Using Names or IDs? The prompt says "from and to take naptan station ID".
				// RouteSections has Originator (ID) and Destination (ID).
				href := fmt.Sprintf("/timetable?line=%s&from=%s&to=%s", l.ID, route.Originator, route.Destination)
				fmt.Fprintf(w, "<li><a href='%s'>%s</a></li>", href, route.Name)
			}
			fmt.Fprint(w, "</ul></li>")
		}
		fmt.Fprint(w, "</ul></body></html>")
	}
}
