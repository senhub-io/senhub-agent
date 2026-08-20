// senhub-agent/internal/agent/formats/event/formatter.go
package event

import (
	"encoding/json"
	"strconv"
	"unicode"

	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"

	"senhub-agent.go/internal/agent/services/agentstate"
	"senhub-agent.go/internal/agent/tags"
	"senhub-agent.go/internal/agent/types/datapoint"
	"senhub-agent.go/internal/agent/types/event"
)

type Formatter struct{}

func NewFormatter() *Formatter {
	return &Formatter{}
}

// syslogSeverityToEventSeverity maps an RFC 5424 severity code (as the
// string the probe put on the tag) to the legacy event severity name.
//
// The eight rungs live in agentstate, once, alongside the OTel mapping
// the same code produces. This used to be a second hand-maintained copy
// of them (#294).
//
// The DEFAULT stays "notice" and is deliberately not unified. It is a
// wire-format behaviour of the legacy /event/insert rail: an input the
// agent cannot parse arrives at the cloud intake as notice today, and
// TestFromEventLog_ByteIdenticalAndStructurePreserved pins that the
// log-bus path reproduces it byte for byte. The OTel rail answers
// Unspecified for the same input — the two rails disagree, on purpose,
// until the legacy rail is retired with its consumers.
func (f *Formatter) syslogSeverityToEventSeverity(syslogSeverity string) event.EventSeverity {
	code, err := strconv.Atoi(syslogSeverity)
	if err != nil {
		return event.Notice
	}
	name := agentstate.SyslogPriorityToEventName(code)
	if name == "" {
		return event.Notice
	}
	return event.EventSeverity(name)
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
