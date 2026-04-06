package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	ics "github.com/arran4/golang-ical"
)

type Rule struct {
	Match 		 string `json:"match"`
	Type  		 string `json:"type"`
	Priority 	 int    `json:"priority"`
	DefaultStart string `json:"default_start"`
	DefaultEnd 	 string `json:"default_end"`

}

type IgnoreRule struct {
	Match string `json:"match"`
}

type Config struct {
	CalendarURL string       `json:"calendar_url"`
	UserFilter  string       `json:"user_filter"`
	DaysAhead   int          `json:"days_ahead"`
	Rules       []Rule       `json:"rules"`
	IgnoreRules []IgnoreRule `json:"ignore_rules"`
}

type RawEvent struct {
	Start   time.Time `json:"start"`
	End     time.Time `json:"end"`
	Summary string    `json:"summary"`
}

type SimplifiedEvent struct {
	Date      string `json:"date"`
	DateHuman string `json:"date_human"`
	Weekday   string `json:"weekday"`
	Type      string `json:"type"`
	Start     string `json:"start"`
	End       string `json:"end"`
	Summary   string `json:"summary"`
}

var cfg Config

func main() {
	loadConfig()

	http.HandleFunc("/health", healthHandler)
	http.HandleFunc("/api/raw", rawHandler)
	http.HandleFunc("/api/schedule", scheduleHandler)
	http.HandleFunc("/api/schedule/today", todayHandler)
	http.HandleFunc("/api/schedule/tomorrow", tomorrowHandler)

	log.Printf("Listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func loadConfig() {
	defaultConfig := Config{
		CalendarURL: "",
		UserFilter:  "",
		DaysAhead:   30,
		Rules: []Rule{
			{Match: "matchA", Type: "replaceA"},
			{Match: "matchB", Type: "replaceB"},
		},
		IgnoreRules: []IgnoreRule{
			{Match: "ignoreA"},
			{Match: "ignoreB"},
		},
	}

	// check if config exists, if not create default and exit
	if _, err := os.Stat("config.json"); os.IsNotExist(err) {
		log.Println("config.json not found, creating default config...")

		data, _ := json.MarshalIndent(defaultConfig, "", "  ")
		if err := os.WriteFile("config.json", data, 0644); err != nil {
			log.Fatalf("failed to create config.json: %v", err)
		}

		log.Fatal("default config.json created, please edit it and restart application")
		cfg = defaultConfig
		return
	}

	// load existing config
	file, err := os.Open("config.json")
	if err != nil {
		log.Fatalf("failed to open config.json: %v", err)
	}
	defer file.Close()

	if err := json.NewDecoder(file).Decode(&cfg); err != nil {
		log.Fatalf("failed to parse config.json: %v", err)
	}

	// set default days ahead if not set or invalid
	if cfg.DaysAhead <= 0 {
		cfg.DaysAhead = 30
	}

	// crash app if calendar URL is not set
	if strings.TrimSpace(cfg.CalendarURL) == "" {
		log.Fatal("calendar_url must be set in config.json")
	}

	// crash appp if no rules are defined	
	if len(cfg.Rules) == 0 {
		log.Fatal("at least one rule must be defined in config.json")
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}

func rawHandler(w http.ResponseWriter, r *http.Request) {
	events, err := fetchAndParseEvents()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, events)
}

func scheduleHandler(w http.ResponseWriter, r *http.Request) {
	events, err := fetchAndParseEvents()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, simplifyEvents(events))
}

func todayHandler(w http.ResponseWriter, r *http.Request) {
	events, err := fetchAndParseEvents()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	today := time.Now().Format("2006-01-02")
	for _, ev := range simplifyEvents(events) {
		if ev.Date == today {
			writeJSON(w, http.StatusOK, ev)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "no schedule found for today"})
}

func tomorrowHandler(w http.ResponseWriter, r *http.Request) {
	events, err := fetchAndParseEvents()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	tomorrow := time.Now().Add(24 * time.Hour).Format("2006-01-02")
	for _, ev := range simplifyEvents(events) {
		if ev.Date == tomorrow {
			writeJSON(w, http.StatusOK, ev)
			return
		}
	}

	writeJSON(w, http.StatusNotFound, map[string]string{"error": "no schedule found for tomorrow"})
}

func fetchAndParseEvents() ([]RawEvent, error) {
	req, err := http.NewRequest("GET", cfg.CalendarURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to build request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch calendar: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("calendar returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read calendar body: %w", err)
	}

	cal, err := ics.ParseCalendar(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ICS: %w", err)
	}

	now := time.Now()
	until := now.AddDate(0, 0, cfg.DaysAhead)

	var events []RawEvent

	for _, event := range cal.Events() {
		summaryProp := event.GetProperty(ics.ComponentPropertySummary)
		if summaryProp == nil {
			continue
		}

		summary := summaryProp.Value
		if cfg.UserFilter != "" && !strings.Contains(strings.ToLower(summary), strings.ToLower(cfg.UserFilter)) {
			continue
		}

		start, err := event.GetStartAt()
		if err != nil {
			continue
		}

		end, err := event.GetEndAt()
		if err != nil {
			continue
		}

		if start.Before(now.AddDate(0, 0, -1)) || start.After(until) {
			continue
		}

		events = append(events, RawEvent{
			Start:   start,
			End:     end,
			Summary: summary,
		})
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Start.Before(events[j].Start)
	})

	return events, nil
}

func simplifyEvents(events []RawEvent) []SimplifiedEvent {
	var out []SimplifiedEvent

	for _, ev := range events {
		serviceType := classifyEvent(ev.Summary)
		if serviceType == "" {
			continue
		}

		start := ev.Start.Format("15:04")
		end := ev.End.Format("15:04")

		out = append(out, SimplifiedEvent{
			Date:      ev.Start.Format("2006-01-02"),
			DateHuman: ev.Start.Format("02/01/2006"),
			Weekday:   dutchWeekday(ev.Start.Weekday()),
			Type:      serviceType,
			Start:     start,
			End:       end,
			Summary:   fmt.Sprintf("%s %s-%s", serviceType, start, end),
		})
	}

	return out
}

func classifyEvent(summary string) string {
	s := strings.ToLower(summary)

	for _, rule := range cfg.Rules {
		if strings.Contains(s, strings.ToLower(rule.Match)) {
			return rule.Type
		}
	}

	return ""
}

func dutchWeekday(w time.Weekday) string {
	switch w {
	case time.Monday:
		return "maandag"
	case time.Tuesday:
		return "dinsdag"
	case time.Wednesday:
		return "woensdag"
	case time.Thursday:
		return "donderdag"
	case time.Friday:
		return "vrijdag"
	case time.Saturday:
		return "zaterdag"
	case time.Sunday:
		return "zondag"
	default:
		return ""
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}