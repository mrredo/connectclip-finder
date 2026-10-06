package matcher_test

import (
	"testing"

	"connectclip-finder/internal/matcher"
	"connectclip-finder/internal/model"
)

func TestMatcherScoring(t *testing.T) {
	m := matcher.NewMatcher()

	tests := []struct {
		name          string
		title         string
		desc          string
		price         float64
		minScore      int
		maxScore      int
		minConfidence model.ConfidenceLevel
	}{
		{
			name:          "Exact Oticon ConnectClip",
			title:         "Oticon ConnectClip",
			desc:          "Pārdodu lietotu bezvadu piederumu",
			price:         85,
			minScore:      85,
			maxScore:      100,
			minConfidence: model.ConfidenceHigh,
		},
		{
			name:          "Oticon Connect Clip with spaces",
			title:         "Oticon Connect Clip Bluetooth microphone",
			desc:          "Wireless audio streamer for Oticon hearing aids",
			price:         95,
			minScore:      85,
			maxScore:      100,
			minConfidence: model.ConfidenceHigh,
		},
		{
			name:          "Latvian diacritics and accessory words",
			title:         "Oticon dzirdes aparātu mikrofons ConnectClip",
			desc:          "Savienotājs un bezvadu straumētājs",
			price:         70,
			minScore:      85,
			maxScore:      100,
			minConfidence: model.ConfidenceHigh,
		},
		{
			name:          "Article number 178509 and AC1A",
			title:         "Oticon 178509 AC1A bezvadu adapteris",
			desc:          "Modelis 178509",
			price:         110,
			minScore:      80,
			maxScore:      100,
			minConfidence: model.ConfidenceHigh,
		},
		{
			name:          "Oticon Bluetooth microphone for hearing aids without clip name",
			title:         "Oticon Bluetooth microphone for hearing aids",
			desc:          "Wireless streamer",
			price:         90,
			minScore:      60,
			maxScore:      80,
			minConfidence: model.ConfidenceMedium,
		},
		{
			name:          "Generic Oticon hearing aid accessory",
			title:         "Oticon dzirdes aparātu aksesuārs",
			desc:          "Piederumi",
			price:         50,
			minScore:      35,
			maxScore:      70,
			minConfidence: model.ConfidenceLow,
		},
		{
			name:          "Negative: Oticon hearing aid charger",
			title:         "Oticon dzirdes aparātu lādētājs charger",
			desc:          "Desk charger for More / Intent",
			price:         60,
			minScore:      0,
			maxScore:      35,
			minConfidence: model.ConfidenceNone,
		},
		{
			name:          "Negative: Oticon TV Adapter",
			title:         "Oticon TV Adapter 3.0",
			desc:          "TV adapter for streaming TV audio",
			price:         80,
			minScore:      0,
			maxScore:      40,
			minConfidence: model.ConfidenceNone,
		},
		{
			name:          "Negative: Unrelated generic Bluetooth microphone",
			title:         "Sony wireless Bluetooth microphone",
			desc:          "For video recording and karaoke",
			price:         45,
			minScore:      0,
			maxScore:      15,
			minConfidence: model.ConfidenceNone,
		},
		{
			name:          "Negative: Phonak hearing aid",
			title:         "Phonak Audeo Paradise dzirdes aparāts",
			desc:          "Labā stāvoklī",
			price:         300,
			minScore:      0,
			maxScore:      10,
			minConfidence: model.ConfidenceNone,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			l := &model.Listing{
				Title:       tt.title,
				Description: tt.desc,
				Price:       tt.price,
			}
			m.Evaluate(l)

			if l.Score < tt.minScore || l.Score > tt.maxScore {
				t.Errorf("Score = %d, want range [%d, %d]. Reasons: %v",
					l.Score, tt.minScore, tt.maxScore, l.MatchReasons)
			}
		})
	}
}

func TestTextNormalization(t *testing.T) {
	raw := "Oticon Dzirdes Aparāts - Lādētājs, 178509!"
	expected := "oticon dzirdes aparats ladetajs 178509"
	actual := matcher.NormalizeText(raw)
	if actual != expected {
		t.Errorf("got %q, want %q", actual, expected)
	}
}

func TestCustomMatcher(t *testing.T) {
	// Configure for Nintendo Switch OLED
	m := matcher.NewCustomMatcher(
		"Nintendo Switch OLED",
		[]string{"Switch OLED", "Nintendo Switch OLED"},
		[]string{"konsole", "console", "joy con"},
		[]string{"HEG-001"},
		[]string{"lite", "spēle", "game only", "vāciņš", "case only"},
		180,
		320,
	)

	// Positive exact match
	l1 := &model.Listing{
		Title:       "Nintendo Switch OLED balta konsole",
		Description: "Modelis HEG-001, pilns komplekts, ideālā stāvoklī",
		Price:       240,
	}
	m.Evaluate(l1)
	if l1.Score < 80 {
		t.Errorf("Expected high score >= 80 for Switch OLED match, got %d. Reasons: %v", l1.Score, l1.MatchReasons)
	}
	if l1.Confidence != model.ConfidenceHigh {
		t.Errorf("Expected ConfidenceHigh, got %s", l1.Confidence)
	}

	// Negative match with excluded keyword
	l2 := &model.Listing{
		Title:       "Nintendo Switch Lite dzeltena",
		Description: "Lietota konsole",
		Price:       120,
	}
	m.Evaluate(l2)
	if l2.Score > 40 {
		t.Errorf("Expected low score <= 40 for Switch Lite with penalty, got %d. Reasons: %v", l2.Score, l2.MatchReasons)
	}

	// Completely unrelated listing
	l3 := &model.Listing{
		Title:       "Apple iPhone 13 128GB",
		Description: "Melns telefons labā stāvoklī",
		Price:       350,
	}
	m.Evaluate(l3)
	if l3.Score != 0 {
		t.Errorf("Expected score 0 for unrelated phone, got %d. Reasons: %v", l3.Score, l3.MatchReasons)
	}
}

