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
)

// Incident struct now perfectly matches the new JSON object structure.
type Incident struct {
	Jurisdiction string  `json:"jurisdiction"`
	Problem      string  `json:"problem"`
	Address      string  `json:"address"`
	Lat          float64 `json:"lat"`
	Long         float64 `json:"long"`
	Timestamp    string  `json:"timestamp"`
}

// --- De-duplication Functions (Unchanged) ---
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

func saveSentIncidents(filename string, sentIDs map[string]bool) error {
	data, err := json.MarshalIndent(sentIDs, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filename, data, 0644)
}

// --- Discord Function (Updated) ---
type DiscordWebhookBody struct {
	Content string `json:"content"`
}

func sendToDiscord(webhookURL string, incident Incident) {
	// Re-added the Google Maps link using the new lat/long data
	message := fmt.Sprintf(
		"🚨 **MVC Alert** 🚨\n\n"+
			"**Problem:** %s\n"+
			"**Address:** %s\n"+
			"**Jurisdiction:** %s\n"+
			"**Time:** %s\n"+
			"**Map Link:** [View on Google Maps](https://www.google.com/maps?q=%.6f,%.6f&z=12)",
		incident.Problem,
		incident.Address,
		incident.Jurisdiction,
		incident.Timestamp,
		incident.Lat,
		incident.Long,
	)
	payload := DiscordWebhookBody{Content: message}
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
	// This should be the correct API URL that provides the JSON
	apiURL := "https://incidents.rwecc.com/getdata"
	webhookURL := "https://discord.com/api/webhooks/1416385101222117446/A02ajmbHisVdUyn1iyRPA1_iVPB_sxvPX_E6uLrdWcMbrNJhkijGS_PXYJM4sx_XLDZQ"
	stateFilename := "sent_rwecc_incidents.json"

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

	// Unmarshal directly into a slice of Incident structs
	var incidents []Incident
	if err := json.Unmarshal(body, &incidents); err != nil {
		log.Fatalf("Error unmarshalling JSON: %s", err)
	}

	log.Println("Searching for new MVC Incidents from API...")
	newAlertsSent := 0

	for _, incident := range incidents {
		// The key for de-duplication remains the same combination
		incidentKey := incident.Timestamp + " " + incident.Address

		// Check the "problem" field for "MVC" and ensure it's a new incident
		if strings.Contains(incident.Problem, "MVC") && !sentIncidents[incidentKey] {
			log.Printf("Found new MVC at %s. Sending to Discord.", incident.Address)
			sendToDiscord(webhookURL, incident)

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