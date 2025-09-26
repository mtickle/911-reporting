package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv" // Library to read .env files
)

// Incident struct matches the JSON object structure from the API.
type Incident struct {
	Jurisdiction string  `json:"jurisdiction"`
	Problem      string  `json:"problem"`
	Address      string  `json:"address"`
	Lat          float64 `json:"lat"`
	Long         float64 `json:"long"`
	Timestamp    string  `json:"timestamp"`
}

// Structs for creating a rich Discord Embed, now with Thumbnail support
type DiscordWebhookPayload struct {
	Username string         `json:"username"`
	Embeds   []DiscordEmbed `json:"embeds"`
}

type DiscordEmbed struct {
	Title     string         `json:"title"`
	Color     int            `json:"color"`
	Fields    []EmbedField   `json:"fields"`
	Footer    EmbedFooter    `json:"footer"`
	Timestamp string         `json:"timestamp"`
	Thumbnail EmbedThumbnail `json:"thumbnail,omitempty"`
}

type EmbedThumbnail struct {
	URL string `json:"url"`
}

type EmbedField struct {
	Name   string `json:"name"`
	Value  string `json:"value"`
	Inline bool   `json:"inline"`
}

type EmbedFooter struct {
	Text string `json:"text"`
}

// loadSentIncidents reads the JSON file of sent alert IDs into a map.
func loadSentIncidents(filename string) (map[string]bool, error) {
	sentIDs := make(map[string]bool)
	data, err := os.ReadFile(filename)
	if os.IsNotExist(err) {
		return sentIDs, nil
	} else if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return sentIDs, nil
	}
	err = json.Unmarshal(data, &sentIDs)
	return sentIDs, err
}

// saveSentIncidents writes the updated map of sent alert IDs back to the file.
func saveSentIncidents(filename string, sentIDs map[string]bool) error {
	data, err := json.MarshalIndent(sentIDs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// sendToDiscord sends a rich embed for a new MVC incident.
func sendToDiscord(webhookURL string, incident Incident, parsedTime time.Time, mapsAPIKey string) {
	// Determine embed color based on the problem description.
	var color int
	problemLower := strings.ToLower(incident.Problem)
	if strings.Contains(problemLower, "injur") {
		color = 15158332 // Red for injuries
	} else if strings.Contains(problemLower, "damage") || strings.Contains(problemLower, "hit & run") {
		color = 15844367 // Yellow for damage/hit & run
	} else {
		color = 3447003 // Default blue for everything else
	}

	// All fields are now single-column for mobile readability.
	fields := []EmbedField{
		{Name: "Address", Value: incident.Address, Inline: false},
		{Name: "Jurisdiction", Value: incident.Jurisdiction, Inline: false},
	}

	embed := DiscordEmbed{
		Title:     incident.Problem,
		Color:     color,
		Fields:    fields,
		Footer:    EmbedFooter{Text: "Fetched from Raleigh-Wake ECC"},
		Timestamp: parsedTime.Format(time.RFC3339),
	}

	// Generate and add the static map thumbnail if an API key is provided.
	if mapsAPIKey != "" {
		mapURL := fmt.Sprintf(
			"https://maps.googleapis.com/maps/api/staticmap?center=%.6f,%.6f&zoom=14&size=300x300&markers=color:red%%7C%.6f,%.6f&key=%s",
			incident.Lat, incident.Long, incident.Lat, incident.Long, mapsAPIKey,
		)
		embed.Thumbnail = EmbedThumbnail{URL: mapURL}
	}

	payload := DiscordWebhookPayload{
		Username: "RWECC MVC Bot",
		Embeds:   []DiscordEmbed{embed},
	}

	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		log.Printf("Error creating JSON payload: %s", err)
		return
	}

	resp, err := http.Post(webhookURL, "application/json", bytes.NewBuffer(jsonPayload))
	if err != nil {
		log.Printf("Error sending to Discord: %s", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		log.Printf("Discord returned non-2xx status: %s", resp.Status)
	}
}

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("Note: .env file not found, reading credentials from environment")
	}

	apiURL := os.Getenv("RWECC_URL")
	webhookURL := os.Getenv("RWECC_DISCORD_HOOK")
	mapsAPIKey := os.Getenv("GOOGLE_MAPS_API_KEY") // Load the new API key
	stateFilename := "sent_rwecc_incidents.json"

	if apiURL == "" || webhookURL == "" {
		log.Fatalln("Error: RWECC_URL and RWECC_DISCORD_HOOK must be set in your environment or .env file.")
	}

	sentIncidents, err := loadSentIncidents(stateFilename)
	if err != nil {
		log.Fatalf("Error loading sent incidents: %s", err)
	}

	resp, err := http.Get(apiURL)
	if err != nil {
		log.Fatalf("Error fetching data from API: %s", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatalf("Error reading API response body: %s", err)
	}

	var incidents []Incident
	if err := json.Unmarshal(body, &incidents); err != nil {
		log.Fatalf("Error unmarshalling JSON: %s", err)
	}

	log.Println("Searching for new MVC Incidents from RWECC API...")
	newAlertsSent := 0

	for _, incident := range incidents {
		incidentKey := incident.Timestamp + " " + incident.Address

		if strings.Contains(incident.Problem, "MVC") && !sentIncidents[incidentKey] {
			log.Printf("Found new MVC at %s. Sending to Discord.", incident.Address)

			loc, _ := time.LoadLocation("America/New_York")
			parsedTime, err := time.Parse("2006-01-02 15:04:05.000", incident.Timestamp)
			if err != nil {
				log.Printf("Error parsing timestamp for incident, using current time. Error: %v", err)
				parsedTime = time.Now()
			}
			easternTime := parsedTime.In(loc)

			sendToDiscord(webhookURL, incident, easternTime, mapsAPIKey)

			sentIncidents[incidentKey] = true
			newAlertsSent++
		}
	}

	if newAlertsSent > 0 {
		if err := saveSentIncidents(stateFilename, sentIncidents); err != nil {
			log.Printf("Error saving sent incidents file: %s", err)
		}
	}
	log.Printf("Search complete. Sent %d new alerts.", newAlertsSent)
}
