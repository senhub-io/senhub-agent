// senhub-agent/internal/agent/formats/event/formatter.go
package event

import (
	"encoding/json"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/types/event"
	"strings"
	"unicode"
)

type Formatter struct{}

func NewFormatter() *Formatter {
	return &Formatter{}
}

// Ajouter dans formatter.go
func (f *Formatter) syslogSeverityToEventSeverity(syslogSeverity string) event.EventSeverity {
	// Syslog severity levels (0-7)
	// 0: Emergency
	// 1: Alert
	// 2: Critical
	// 3: Error
	// 4: Warning
	// 5: Notice
	// 6: Informational
	// 7: Debug
	switch syslogSeverity {
	case "0":
		return event.Emergency
	case "1":
		return event.Alert
	case "2":
		return event.Critical
	case "3":
		return event.Error
	case "4":
		return event.Warning
	case "5":
		return event.Notice
	case "6":
		return event.Informational
	case "7":
		return event.Debug
	default:
		return event.Notice
	}
}

// FormatDataPoint convertit un DataPoint en EventDataPoint
func (f *Formatter) FormatDataPoint(dp datapoint.DataPoint) event.EventDataPoint {
	// Créer un nouveau point d'événement
	eventData := make(event.EventDataPoint)

	// Ajouter les champs obligatoires
	eventData["timestamp"] = dp.Timestamp
	eventData["host"] = f.sanitizeUTF8(f.getTagValue(dp.Tags, "host"))

	// Convertir la severity syslog en EventSeverity puis en string
	severityStr := f.getTagValue(dp.Tags, "severity")
	eventSeverity := f.syslogSeverityToEventSeverity(severityStr)
	eventData["severity"] = string(eventSeverity)

	eventData["message"] = f.sanitizeUTF8(f.getTagValue(dp.Tags, "message"))

	// Extraire les valeurs complexes si elles existent
	complexValuesJSON := f.getTagValue(dp.Tags, "_complex_values")
	if complexValuesJSON != "" {
		var complexValues map[string]interface{}
		if err := json.Unmarshal([]byte(complexValuesJSON), &complexValues); err == nil {
			// Ajouter les valeurs complexes directement à l'événement
			for key, value := range complexValues {
				eventData[key] = value // Préserve les tableaux et structures imbriquées
			}
		}
	}

	// Ajouter tous les autres tags comme champs dynamiques (en excluant les métadonnées spéciales)
	for _, tag := range dp.Tags {
		if tag.Key != "host" && tag.Key != "severity" && tag.Key != "message" && 
		   tag.Key != "_complex_values" && !eventData.HasKey(tag.Key) {
			eventData[tag.Key] = f.sanitizeUTF8(tag.Value)
		}
	}

	// Ajouter la valeur si elle n'est pas nulle
	if dp.Value != 0 {
		eventData["value"] = dp.Value
	}

	return eventData
}

// getTagValue récupère la valeur d'un tag par sa clé
func (f *Formatter) getTagValue(tags []tags.Tag, key string) string {
	for _, tag := range tags {
		if tag.Key == key {
			return tag.Value
		}
	}
	return ""
}

// getSeverity convertit une chaîne en niveau de sévérité valide
func (f *Formatter) getSeverity(value string) event.EventSeverity {
	switch strings.ToLower(value) {
	case "emerg", "emergency":
		return event.Emergency
	case "alert":
		return event.Alert
	case "crit", "critical":
		return event.Critical
	case "err", "error":
		return event.Error
	case "warning", "warn":
		return event.Warning
	case "notice":
		return event.Notice
	case "info", "information":
		return event.Informational
	case "debug":
		return event.Debug
	default:
		return event.Notice
	}
}

// sanitizeUTF8 nettoie et normalise une chaîne en UTF-8
func (f *Formatter) sanitizeUTF8(input string) string {
	t := transform.Chain(norm.NFC)
	result, _, _ := transform.String(t, input)

	// Replace any remaining non-UTF8 chars with space
	runes := []rune(result)
	for i, r := range runes {
		if !unicode.IsPrint(r) {
			runes[i] = ' '
		}
	}
	return string(runes)
}
