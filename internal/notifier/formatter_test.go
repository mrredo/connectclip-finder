package notifier_test

import (
	"strings"
	"testing"

	"connectclip-finder/internal/model"
	"connectclip-finder/internal/notifier"
)

func TestFormatTelegramMessage(t *testing.T) {
	listing := &model.Listing{
		Source:       model.SourceSSCom,
		Title:        "Oticon ConnectClip mikrofons",
		Price:        85.0,
		Currency:     "EUR",
		Location:     "Rīga",
		Score:        92,
		Confidence:   model.ConfidenceHigh,
		MatchReasons: []string{"Matched 'Oticon ConnectClip' in text (+85)", "Plausible price (+10)"},
		URL:          "https://www.ss.com/msg/lv/example.html",
	}

	msg := notifier.FormatTelegramMessage(listing)

	requiredStrings := []string{
		"Oticon ConnectClip mikrofons",
		"85.00",
		"ss.com",
		"Rīga",
		"92%",
		"HIGH",
		"https://www.ss.com/msg/lv/example.html",
	}

	for _, req := range requiredStrings {
		if !strings.Contains(msg, req) {
			t.Errorf("expected formatted message to contain %q, but got:\n%s", req, msg)
		}
	}
}
