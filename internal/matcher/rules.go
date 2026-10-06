package matcher

import (
	"strings"
	"unicode"
)

// NormalizeText strips accents, lowercases, and normalizes spacing.
// Supports Latvian diacritics mapping.
func NormalizeText(s string) string {
	s = strings.ToLower(s)

	replacer := strings.NewReplacer(
		"ā", "a", "č", "c", "ē", "e", "ģ", "g",
		"ī", "i", "ķ", "k", "ļ", "l", "ņ", "n",
		"š", "s", "ū", "u", "ž", "z",
		"-", " ", "_", " ", "/", " ", "\\", " ",
		",", " ", ".", " ", "(", " ", ")", " ",
		":", " ", ";", " ", "\"", " ", "'", " ",
		"!", " ", "?", " ", "[", " ", "]", " ",
		"{", " ", "}", " ", "+", " ", "*", " ",
	)
	s = replacer.Replace(s)

	var sb strings.Builder
	inSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !inSpace {
				sb.WriteRune(' ')
				inSpace = true
			}
		} else {
			sb.WriteRune(r)
			inSpace = false
		}
	}
	return strings.TrimSpace(sb.String())
}

// Rule defines a scoring criterion with positive or negative weight.
type Rule struct {
	Name   string
	Weight int
	Check  func(title, desc string, price float64) (bool, string)
}

// BuildRules returns the standard set of rules for Oticon ConnectClip detection.
func BuildRules() []Rule {
	return []Rule{
		// 1. Direct exact / strong matches
		{
			Name:   "Exact 'Oticon ConnectClip' phrase",
			Weight: 85,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if strings.Contains(t, "oticon connectclip") || strings.Contains(t, "oticon connect clip") {
					return true, "Matched 'Oticon ConnectClip' in text (+85)"
				}
				return false, ""
			},
		},
		{
			Name:   "Specific product part number / model (178509, AC1A, 2ACAHAC01)",
			Weight: 75,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if strings.Contains(t, "178509") || strings.Contains(t, "ac1a") || strings.Contains(t, "2acahac01") {
					return true, "Matched Oticon ConnectClip model/part number (+75)"
				}
				return false, ""
			},
		},
		{
			Name:   "ConnectClip name without Oticon in immediate phrase",
			Weight: 50,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if (strings.Contains(t, "connectclip") || strings.Contains(t, "connect clip")) &&
					!strings.Contains(t, "oticon connectclip") && !strings.Contains(t, "oticon connect clip") {
					return true, "Matched 'ConnectClip' name (+50)"
				}
				return false, ""
			},
		},
		{
			Name:   "Oticon Brand presence",
			Weight: 20,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if strings.Contains(t, "oticon") &&
					!strings.Contains(t, "oticon connectclip") && !strings.Contains(t, "oticon connect clip") {
					return true, "Matched 'Oticon' brand (+20)"
				}
				return false, ""
			},
		},
		{
			Name:   "Hearing aid accessory context",
			Weight: 15,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				hasHearingAid := strings.Contains(t, "dzirdes aparat") || strings.Contains(t, "hearing aid") ||
					strings.Contains(t, "sluhovoj apparat") || strings.Contains(t, "слуховой аппарат")
				if hasHearingAid {
					return true, "Matched hearing aid context (+15)"
				}
				return false, ""
			},
		},
		{
			Name:   "Audio streaming / microphone functionality with hearing aid or Oticon context",
			Weight: 20,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				hasStreamer := strings.Contains(t, "streamer") || strings.Contains(t, "straumetaj") ||
					strings.Contains(t, "mikrofon") || strings.Contains(t, "microphone") ||
					strings.Contains(t, "savienotaj")
				hasContext := strings.Contains(t, "oticon") || strings.Contains(t, "dzirdes") || strings.Contains(t, "hearing aid")
				if hasStreamer && hasContext {
					return true, "Matched audio streaming / microphone adapter context (+20)"
				}
				return false, ""
			},
		},
		{
			Name:   "Bluetooth specification with hearing aid or Oticon context",
			Weight: 10,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				hasBT := strings.Contains(t, "bluetooth") || strings.Contains(t, "blutooth") || strings.Contains(t, "bt")
				hasContext := strings.Contains(t, "oticon") || strings.Contains(t, "dzirdes") || strings.Contains(t, "hearing aid")
				if hasBT && hasContext {
					return true, "Matched Bluetooth feature with hearing aid context (+10)"
				}
				return false, ""
			},
		},

		// 2. Realistic price signal (only applies if at least some relevant context exists)
		{
			Name:   "Plausible second-hand price range (€30 - €200)",
			Weight: 10,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				hasAnyContext := strings.Contains(t, "oticon") || strings.Contains(t, "connectclip") ||
					strings.Contains(t, "connect clip") || strings.Contains(t, "dzirdes")
				if hasAnyContext && price >= 30 && price <= 200 {
					return true, "Price is within plausible second-hand range €30-€200 (+10)"
				}
				return false, ""
			},
		},

		// 3. Negative signals (penalties for different products)
		{
			Name:   "Penalty: No Oticon or hearing aid reference",
			Weight: -60,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				hasRelevantBrandOrDevice := strings.Contains(t, "oticon") ||
					strings.Contains(t, "connectclip") || strings.Contains(t, "connect clip") ||
					strings.Contains(t, "178509") || strings.Contains(t, "ac1a") ||
					strings.Contains(t, "dzirdes") || strings.Contains(t, "hearing aid")
				if !hasRelevantBrandOrDevice {
					return true, "No Oticon or hearing-aid reference found (-60)"
				}
				return false, ""
			},
		},
		{
			Name:   "Penalty: Hearing aid charger / lādētājs",
			Weight: -45,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if strings.Contains(t, "ladetaj") || strings.Contains(t, "charger") || strings.Contains(t, "smartcharger") {
					if !strings.Contains(t, "connectclip") && !strings.Contains(t, "connect clip") {
						return true, "Identified as hearing aid charger (-45)"
					}
				}
				return false, ""
			},
		},
		{
			Name:   "Penalty: TV Adapter (TV-A)",
			Weight: -35,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if strings.Contains(t, "tv adapter") || strings.Contains(t, "tv a") || strings.Contains(t, "tvbox") {
					if !strings.Contains(t, "connectclip") {
						return true, "Identified as Oticon TV Adapter (-35)"
					}
				}
				return false, ""
			},
		},
		{
			Name:   "Penalty: EduMic classroom microphone",
			Weight: -35,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if strings.Contains(t, "edumic") || strings.Contains(t, "edu mic") {
					return true, "Identified as Oticon EduMic (-35)"
				}
				return false, ""
			},
		},
		{
			Name:   "Penalty: Competing hearing aid brands without Oticon",
			Weight: -60,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if (strings.Contains(t, "phonak") || strings.Contains(t, "widex") ||
					strings.Contains(t, "signia") || strings.Contains(t, "starkey") ||
					strings.Contains(t, "resound")) && !strings.Contains(t, "oticon") {
					return true, "Different hearing aid manufacturer brand (-60)"
				}
				return false, ""
			},
		},
		{
			Name:   "Penalty: Implausible high price for single accessory (> €550)",
			Weight: -15,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if price > 550 && !strings.Contains(t, "connectclip") {
					return true, "High price suggests full hearing aid set rather than clip (-15)"
				}
				return false, ""
			},
		},
		{
			Name:   "Penalty: Implausible low price (< €10, likely filters/domes)",
			Weight: -20,
			Check: func(title, desc string, price float64) (bool, string) {
				t := title + " " + desc
				if price > 0 && price < 10 && !strings.Contains(t, "connectclip") {
					return true, "Very low price (< €10) suggests wax guards/batteries/domes (-20)"
				}
				return false, ""
			},
		},
	}
}
