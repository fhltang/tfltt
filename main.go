package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"tfltt/tfl/client"
	"tfltt/tfl/client/line"
	"tfltt/tfl/models"

	"github.com/go-openapi/runtime"
	httptransport "github.com/go-openapi/runtime/client"
	"github.com/go-openapi/strfmt"
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

	http.HandleFunc("/{$}", DefaultHandler(tflClient))

	http.HandleFunc("/timetable", TimetableHandler(tflClient))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on :%s...", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
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
			var sb strings.Builder
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			fmt.Fprintf(w, "<html><body><h1>Timetable for %s from %s to %s</h1>", lineID, fromID, toID)

			if payload.Timetable != nil {
				for _, route := range payload.Timetable.Routes {
					for _, schedule := range route.Schedules {
						renderer, err := NewTimetableRenderer(payload, route, schedule)
						if err != nil {
							fmt.Fprintf(&sb, "<p>Error rendering schedule %s: %v</p>", schedule.Name, err)
							continue
						}
						output := renderer.RenderAsText(200, 50)
						fmt.Fprintf(&sb, "<h2>Schedule: %s</h2><pre>%s</pre>", schedule.Name, output)
					}
				}
			}

			if sb.Len() > 0 {
				fmt.Fprint(w, sb.String())
			} else {
				fmt.Fprint(w, "<p>No schedules found.</p>")
			}
			fmt.Fprint(w, "</body></html>")
		} else {
			http.Error(w, "No timetable payload received", http.StatusNoContent)
		}
	}
}

func DefaultHandler(tflClient *client.Tfl) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		params := line.NewLineRouteByModeParams()
		params.Modes = []string{"tube"}

		resp, err := tflClient.Line.LineRouteByMode(params)
		if err != nil {
			http.Error(w, fmt.Sprintf("Error fetching routes: %v", err), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, "<html><head><style>table { border-collapse: collapse; width: 100%; } th, td { border: 1px solid black; padding: 8px; text-align: left; } th { background-color: #f2f2f2; }</style></head><body>")
		fmt.Fprint(w, "<h1>Tube Lines and Routes</h1>")
		fmt.Fprint(w, "<table>")
		fmt.Fprint(w, "<thead><tr><th>Line</th><th>Outbound</th><th>Inbound</th></tr></thead>")
		fmt.Fprint(w, "<tbody>")

		for _, l := range resp.Payload {
			// Group routes by segment (Origin <-> Destination)
			type routePair struct {
				Outbound *models.TflAPIPresentationEntitiesMatchedRoute
				Inbound  *models.TflAPIPresentationEntitiesMatchedRoute
			}

			segments := make(map[string]*routePair)
			var segmentKeys []string

			for _, route := range l.RouteSections {
				// Create a unique key for the segment, independent of direction
				key := route.Originator + "-" + route.Destination
				if route.Originator > route.Destination {
					key = route.Destination + "-" + route.Originator
				}

				if _, exists := segments[key]; !exists {
					segments[key] = &routePair{}
					segmentKeys = append(segmentKeys, key)
				}

				pair := segments[key]
				if strings.ToLower(route.Direction) == "outbound" {
					pair.Outbound = route
				} else if strings.ToLower(route.Direction) == "inbound" {
					pair.Inbound = route
				} else {
					if pair.Outbound == nil {
						pair.Outbound = route
					} else {
						pair.Inbound = route
					}
				}
			}

			// Render rows for this line
			firstRow := true
			for _, key := range segmentKeys {
				pair := segments[key]
				fmt.Fprint(w, "<tr>")
				if firstRow {
					fmt.Fprintf(w, "<td rowspan='%d'>%s</td>", len(segmentKeys), l.Name)
					firstRow = false
				}

				// Outbound Cell
				fmt.Fprint(w, "<td>")
				if pair.Outbound != nil {
					href := fmt.Sprintf("/timetable?line=%s&from=%s&to=%s", l.ID, pair.Outbound.Originator, pair.Outbound.Destination)
					fmt.Fprintf(w, "<a href='%s'>%s</a>", href, pair.Outbound.Name)
				}
				fmt.Fprint(w, "</td>")

				// Inbound Cell
				fmt.Fprint(w, "<td>")
				if pair.Inbound != nil {
					href := fmt.Sprintf("/timetable?line=%s&from=%s&to=%s", l.ID, pair.Inbound.Originator, pair.Inbound.Destination)
					fmt.Fprintf(w, "<a href='%s'>%s</a>", href, pair.Inbound.Name)
				}
				fmt.Fprint(w, "</td>")

				fmt.Fprint(w, "</tr>")
			}
		}
		fmt.Fprint(w, "</tbody></table></body></html>")
	}
}
