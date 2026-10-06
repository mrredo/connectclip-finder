package matcher

import (
	"fmt"
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

// BuildCustomRules constructs dynamic matching rules for any user-configured target item.
func BuildCustomRules(
	targetName string,
	exactKeywords []string,
	contextKeywords []string,
	modelNumbers []string,
	excludeKeywords []string,
	minPrice, maxPrice float64,
) []Rule {
	var rules []Rule

	// 1. Exact phrase matches (+85)
	if len(exactKeywords) > 0 {
		var normExact []string
		for _, kw := range exactKeywords {
			norm := NormalizeText(kw)
			if norm != "" {
				normExact = append(normExact, norm)
			}
		}
		if len(normExact) > 0 {
			rules = append(rules, Rule{
				Name:   "Exact target item phrase",
				Weight: 85,
				Check: func(title, desc string, price float64) (bool, string) {
					t := title + " " + desc
					for _, phrase := range normExact {
						if strings.Contains(t, phrase) {
							return true, fmt.Sprintf("Matched exact target phrase '%s' in text (+85)", phrase)
						}
					}
					return false, ""
				},
			})
		}
	}

	// 2. Model / part numbers (+75)
	if len(modelNumbers) > 0 {
		var normModels []string
		for _, mn := range modelNumbers {
			norm := NormalizeText(mn)
			if norm != "" {
				normModels = append(normModels, norm)
			}
		}
		if len(normModels) > 0 {
			rules = append(rules, Rule{
				Name:   "Model / article / part number",
				Weight: 75,
				Check: func(title, desc string, price float64) (bool, string) {
					t := title + " " + desc
					for _, m := range normModels {
						if strings.Contains(t, m) {
							return true, fmt.Sprintf("Matched model/part number '%s' (+75)", m)
						}
					}
					return false, ""
				},
			})
		}
	}

	// 3. Context keywords (+20 each)
	if len(contextKeywords) > 0 {
		var normContext []string
		for _, kw := range contextKeywords {
			norm := NormalizeText(kw)
			if norm != "" {
				normContext = append(normContext, norm)
			}
		}
		for _, kw := range normContext {
			keyword := kw
			rules = append(rules, Rule{
				Name:   fmt.Sprintf("Context signal '%s'", keyword),
				Weight: 20,
				Check: func(title, desc string, price float64) (bool, string) {
					t := title + " " + desc
					if strings.Contains(t, keyword) {
						return true, fmt.Sprintf("Matched context signal '%s' (+20)", keyword)
					}
					return false, ""
				},
			})
		}
	}

	// 4. Plausible target price range (+15)
	if minPrice > 0 || maxPrice > 0 {
		rules = append(rules, Rule{
			Name:   "Plausible target price range",
			Weight: 15,
			Check: func(title, desc string, price float64) (bool, string) {
				if price <= 0 {
					return false, ""
				}
				if (minPrice <= 0 || price >= minPrice) && (maxPrice <= 0 || price <= maxPrice) {
					return true, fmt.Sprintf("Price €%.0f is within target range [€%.0f–€%.0f] (+15)", price, minPrice, maxPrice)
				}
				return false, ""
			},
		})
	}

	// 5. Excluded negative keywords (-50 each)
	if len(excludeKeywords) > 0 {
		var normExclude []string
		for _, kw := range excludeKeywords {
			norm := NormalizeText(kw)
			if norm != "" {
				normExclude = append(normExclude, norm)
			}
		}
		for _, kw := range normExclude {
			badWord := kw
			rules = append(rules, Rule{
				Name:   fmt.Sprintf("Excluded keyword '%s'", badWord),
				Weight: -50,
				Check: func(title, desc string, price float64) (bool, string) {
					t := title + " " + desc
					if strings.Contains(t, badWord) {
						return true, fmt.Sprintf("Excluded keyword '%s' (-50)", badWord)
					}
					return false, ""
				},
			})
		}
	}

	// 6. Generic penalty if no target terms matched at all
	rules = append(rules, Rule{
		Name:   "Penalty: No target keywords matched",
		Weight: -60,
		Check: func(title, desc string, price float64) (bool, string) {
			t := title + " " + desc
			var anyFound bool
			for _, kw := range exactKeywords {
				if strings.Contains(t, NormalizeText(kw)) {
					anyFound = true
					break
				}
			}
			if !anyFound {
				for _, kw := range contextKeywords {
					if strings.Contains(t, NormalizeText(kw)) {
						anyFound = true
						break
					}
				}
			}
			if !anyFound {
				for _, mn := range modelNumbers {
					if strings.Contains(t, NormalizeText(mn)) {
						anyFound = true
						break
					}
				}
			}
			if !anyFound {
				return true, "No relevant target keywords found (-60)"
			}
			return false, ""
		},
	})

	return rules
}
