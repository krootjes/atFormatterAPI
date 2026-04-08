package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
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
	APIKey      string       `json:"api_key"`
	CalendarURL string       `json:"calendar_url"`
	UserFilter  string       `json:"user_filter"`
	DaysAhead   int          `json:"days_ahead"`
	Weekdays    [7]string    `json:"weekdays"`
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

var (
	cacheMutex 	 sync.Mutex
	cachedEvents []RawEvent
	cacheTime 	 time.Time
)

const cacheTTL = 15 * time.Minute

func main() {
	loadConfig()

	http.HandleFunc("/health", 				  authMiddleware(healthHandler))
	http.HandleFunc("/api/raw",    			  authMiddleware(rawHandler))
	http.HandleFunc("/api/raw/ics", 		  authMiddleware(rawICSHandler))
	http.HandleFunc("/api/schedule", 		  authMiddleware(scheduleHandler))
	http.HandleFunc("/api/schedule/today", 	  authMiddleware(todayHandler))
	http.HandleFunc("/api/schedule/tomorrow", authMiddleware(tomorrowHandler))

	log.Printf("Listening on 0.0.0.0:8080 (container internal port)")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func loadConfig() {
	defaultConfig := Config{
		CalendarURL: "",
		UserFilter:  "",
		DaysAhead:   30,
		Weekdays:    [7]string{"sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday"},
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

func authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		if cfg.APIKey == "" {
		next.ServeHTTP(w, r)
		return
		}

		apiKey := r.Header.Get("X-API-Key")
		if apiKey != cfg.APIKey {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	_, err := getCachedEvents()


	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"calendar_reachable": err == nil,
		"last_fetch": cacheTime.Format(time.RFC3339),
	})

}

func rawHandler(w http.ResponseWriter, r *http.Request) {
	events, err := getCachedEvents()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, events)
}

func rawICSHandler(w http.ResponseWriter, r *http.Request) {
	body, err := fetchICSBody()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(body)
}

func scheduleHandler(w http.ResponseWriter, r *http.Request) {
	events, err := getCachedEvents()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"data": simplifyEvents(events),
	})
}

func todayHandler(w http.ResponseWriter, r *http.Request) {
	events, err := getCachedEvents()
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
	events, err := getCachedEvents()
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

func fetchICSBody() ([]byte, error) {
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

	return body, nil
}

func fetchAndParseEvents() ([]RawEvent, error) {
	body, err := fetchICSBody()
	if err != nil {
		return nil, err
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

func getCachedEvents() ([]RawEvent, error) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if cachedEvents != nil && time.Since(cacheTime) < cacheTTL {
		return cachedEvents, nil
	}
	var err error
	cachedEvents, err = fetchAndParseEvents()
	cacheTime = time.Now()
	return cachedEvents, err
}

var timeRangeRegex = regexp.MustCompile(`(\d{2}:\d{2})-(\d{2}:\d{2})`)

func shouldIgnore(summary string) bool {
	s := strings.ToLower(summary)
	for _, rule := range cfg.IgnoreRules {
		if strings.Contains(s, strings.ToLower(rule.Match)) {
			return true
		}
	}
	return false
}

func matchRule(summary string) *Rule {
	s := strings.ToLower(summary)
	var best *Rule
	for i := range cfg.Rules {
		r := &cfg.Rules[i]
		if strings.Contains(s, strings.ToLower(r.Match)) {
			if best == nil || r.Priority > best.Priority {
				best = r
			}
		}
	}
	return best
}

func resolveTimes(summary string, rule Rule) (string, string) {
	if m := timeRangeRegex.FindStringSubmatch(summary); m != nil {
		return m[1], m[2]
	}
	return rule.DefaultStart, rule.DefaultEnd
}

func simplifyEvents(events []RawEvent) []SimplifiedEvent {
	type candidate struct {
		event RawEvent
		rule  Rule
	}

	best := make(map[string]candidate)

	for _, ev := range events {
		if shouldIgnore(ev.Summary) {
			continue
		}

		rule := matchRule(ev.Summary)
		if rule == nil {
			continue
		}

		date := ev.Start.Format("2006-01-02")
		existing, ok := best[date]
		if !ok || rule.Priority > existing.rule.Priority {
			best[date] = candidate{event: ev, rule: *rule}
		}
	}

	dates := make([]string, 0, len(best))
	for d := range best {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	var out []SimplifiedEvent
	for _, date := range dates {
		c := best[date]
		start, end := resolveTimes(c.event.Summary, c.rule)
		out = append(out, SimplifiedEvent{
			Date:      date,
			DateHuman: c.event.Start.Format("02/01/2006"),
			Weekday:   cfg.Weekdays[c.event.Start.Weekday()],
			Type:      c.rule.Type,
			Start:     start,
			End:       end,
			Summary:   fmt.Sprintf("%s %s-%s", c.rule.Type, start, end),
		})
	}

	return out
}


func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}