package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
	_ "time/tzdata"
)

const icsBaseURL = "https://servicelayer3c.azure-api.net/wastecalendar/calendar/ical/%s"
const port = "8080"

var londonTZ *time.Location

func init() {
	var err error
	londonTZ, err = time.LoadLocation("Europe/London")
	if err != nil {
		log.Fatalf("failed to load Europe/London timezone: %v", err)
	}
}

type Bin struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

type Collection struct {
	Date    string `json:"date"`
	Bins    []Bin  `json:"bins"`
	Display string `json:"display"` // pre-formatted emoji string, e.g. "🟢 Green · 🔵 Blue"
}

type Response struct {
	HouseID  string       `json:"house_id"`
	Next     *Collection  `json:"next"`
	Upcoming []Collection `json:"upcoming"`
	CachedAt time.Time    `json:"cached_at"`
}

type cacheEntry struct {
	collections []Collection
	lastDate    time.Time
	cachedAt    time.Time
}

var cache sync.Map

func binEmoji(color string) string {
	switch color {
	case "green":
		return "🟢"
	case "blue":
		return "🔵"
	case "black":
		return "⚫"
	case "brown":
		return "🟤"
	case "orange":
		return "🟠"
	case "grey":
		return "🔘"
	default:
		return "🗑️"
	}
}

func binColor(summary string) string {
	s := strings.ToLower(summary)
	switch {
	case strings.Contains(s, "green"):
		return "green"
	case strings.Contains(s, "blue"):
		return "blue"
	case strings.Contains(s, "black"):
		return "black"
	case strings.Contains(s, "brown"):
		return "brown"
	case strings.Contains(s, "food"):
		return "orange"
	case strings.Contains(s, "grey"), strings.Contains(s, "gray"):
		return "grey"
	default:
		return "white"
	}
}

func parseICS(r io.Reader) ([]Collection, time.Time) {
	byDate := map[string][]Bin{}
	var dates []string

	scanner := bufio.NewScanner(r)
	var inEvent bool
	var currentDate, currentSummary string

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "BEGIN:VEVENT":
			inEvent = true
			currentDate, currentSummary = "", ""

		case line == "END:VEVENT":
			if inEvent && currentDate != "" && currentSummary != "" {
				if _, exists := byDate[currentDate]; !exists {
					dates = append(dates, currentDate)
				}
				byDate[currentDate] = append(byDate[currentDate], Bin{
					Name:  currentSummary,
					Color: binColor(currentSummary),
				})
			}
			inEvent = false

		case inEvent && strings.HasPrefix(line, "DTSTART"):
			if idx := strings.LastIndex(line, ":"); idx >= 0 {
				currentDate = strings.TrimSpace(line[idx+1:])
			}

		case inEvent && strings.HasPrefix(line, "SUMMARY:"):
			currentSummary = strings.TrimPrefix(line, "SUMMARY:")
		}
	}

	if len(dates) == 0 {
		return nil, time.Time{}
	}

	sort.Strings(dates)

	collections := make([]Collection, 0, len(dates))
	for _, d := range dates {
		bins := byDate[d]
		parts := make([]string, len(bins))
		for i, b := range bins {
			parts[i] = binEmoji(b.Color) + " " + b.Name
		}
		collections = append(collections, Collection{
			Date:    fmt.Sprintf("%s-%s-%s", d[0:4], d[4:6], d[6:8]),
			Bins:    bins,
			Display: strings.Join(parts, " · "),
		})
	}

	last := dates[len(dates)-1]
	lastDate, err := time.ParseInLocation("20060102", last, londonTZ)
	if err != nil {
		return collections, time.Time{}
	}

	return collections, lastDate
}

func fetchAndCache(houseID string) (cacheEntry, error) {
	url := fmt.Sprintf(icsBaseURL, houseID)
	resp, err := http.Get(url) //nolint:gosec
	if err != nil {
		return cacheEntry{}, fmt.Errorf("fetching ICS: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return cacheEntry{}, fmt.Errorf("upstream returned %d", resp.StatusCode)
	}

	cols, lastDate := parseICS(resp.Body)
	entry := cacheEntry{
		collections: cols,
		lastDate:    lastDate,
		cachedAt:    time.Now().UTC(),
	}
	cache.Store(houseID, entry)
	return entry, nil
}

func getEntry(houseID string) (cacheEntry, error) {
	now := time.Now().In(londonTZ)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, londonTZ)

	if v, ok := cache.Load(houseID); ok {
		entry := v.(cacheEntry)
		// Cache is valid while the last event date hasn't passed.
		// A zero lastDate (empty ICS) is treated as expired immediately.
		if !entry.lastDate.IsZero() && !entry.lastDate.Before(today) {
			return entry, nil
		}
	}

	return fetchAndCache(houseID)
}

func futureCollections(all []Collection) []Collection {
	now := time.Now().In(londonTZ)
	todayStr := fmt.Sprintf("%04d-%02d-%02d", now.Year(), now.Month(), now.Day())

	var result []Collection
	for _, c := range all {
		if c.Date >= todayStr {
			result = append(result, c)
		}
	}
	return result
}

func handleCollections(w http.ResponseWriter, r *http.Request) {
	houseID := strings.TrimPrefix(r.URL.Path, "/collections/")
	if houseID == "" || strings.Contains(houseID, "/") {
		http.Error(w, "invalid house_id", http.StatusBadRequest)
		return
	}

	entry, err := getEntry(houseID)
	if err != nil {
		log.Printf("error for house %s: %v", houseID, err)
		http.Error(w, "failed to fetch collection data", http.StatusBadGateway)
		return
	}

	upcoming := futureCollections(entry.collections)
	resp := Response{
		HouseID:  houseID,
		Upcoming: upcoming,
		CachedAt: entry.cachedAt,
	}
	if len(upcoming) > 0 {
		resp.Next = &upcoming[0]
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	err = json.NewEncoder(w).Encode(resp)
	if err != nil {
		log.Printf("failed to encode response json: %v", err)
	}
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/collections/", handleCollections)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	log.Printf("listening on :%s", port)
	log.Fatal(http.ListenAndServe(":"+port, mux))
}
