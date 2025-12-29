package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"tfltt/tfl/client"
	"tfltt/tfl/client/line"

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

	http.HandleFunc("/", DefaultHandler(tflClient))

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
