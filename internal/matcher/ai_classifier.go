package matcher

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"connectclip-finder/internal/model"
)

// AIClassifier handles optional LLM candidate verification.
type AIClassifier struct {
	enabled  bool
	provider string
	apiKey   string
	model    string
	client   *http.Client
}

// NewAIClassifier initializes an AI classifier.
func NewAIClassifier(enabled bool, provider, apiKey, modelName string) *AIClassifier {
	return &AIClassifier{
		enabled:  enabled && apiKey != "",
		provider: strings.ToLower(provider),
		apiKey:   apiKey,
		model:    modelName,
		client: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

// IsEnabled returns whether the classifier is configured and active.
func (c *AIClassifier) IsEnabled() bool {
	return c.enabled
}

type aiResponse struct {
	IsMatch     bool   `json:"is_match"`
	Confidence  int    `json:"confidence"`
	Explanation string `json:"explanation"`
}

// Classify verifies ambiguous candidates using Gemini or OpenAI format.
func (c *AIClassifier) Classify(ctx context.Context, listing *model.Listing) error {
	if !c.enabled {
		return nil
	}

	prompt := fmt.Sprintf(`You are an expert audio accessory classifier.
The target device is an "Oticon ConnectClip", a small wireless Bluetooth microphone and streamer accessory for Oticon hearing aids (approx €300 new, part number 178509, model AC1A).
Distinguish it from hearing aids themselves, chargers/docks, TV adapters, or other brands (Phonak, Widex).

Evaluate this Latvian marketplace listing:
Title: %s
Description: %s
Price: €%.2f

Respond ONLY in JSON with this schema:
{
  "is_match": boolean,
  "confidence": integer from 0 to 100,
  "explanation": "concise rationale"
}`, listing.Title, listing.Description, listing.Price)

	var reqBody []byte
	var reqURL string
	var req *http.Request
	var err error

	if c.provider == "gemini" {
		reqURL = fmt.Sprintf("https://generativelanguage.googleapis.com/v1beta/models/%s:generateContent?key=%s", c.model, c.apiKey)
		payload := map[string]any{
			"contents": []map[string]any{
				{
					"parts": []map[string]string{
						{"text": prompt},
					},
				},
			},
			"generationConfig": map[string]any{
				"responseMimeType": "application/json",
				"temperature":      0.1,
			},
		}
		reqBody, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		req, err = http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(reqBody))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		// OpenAI compatible format
		reqURL = "https://api.openai.com/v1/chat/completions"
		payload := map[string]any{
			"model": c.model,
			"messages": []map[string]string{
				{"role": "user", "content": prompt},
			},
			"response_format": map[string]string{"type": "json_object"},
			"temperature":     0.1,
		}
		reqBody, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		req, err = http.NewRequestWithContext(ctx, "POST", reqURL, bytes.NewReader(reqBody))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("AI API call failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("AI API error %d: %s", resp.StatusCode, string(b))
	}

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	var jsonText string
	if c.provider == "gemini" {
		var geminiResp struct {
			Candidates []struct {
				Content struct {
					Parts []struct {
						Text string `json:"text"`
					} `json:"parts"`
				} `json:"content"`
			} `json:"candidates"`
		}
		if err := json.Unmarshal(respBytes, &geminiResp); err != nil {
			return err
		}
		if len(geminiResp.Candidates) > 0 && len(geminiResp.Candidates[0].Content.Parts) > 0 {
			jsonText = geminiResp.Candidates[0].Content.Parts[0].Text
		}
	} else {
		var openaiResp struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
			} `json:"choices"`
		}
		if err := json.Unmarshal(respBytes, &openaiResp); err != nil {
			return err
		}
		if len(openaiResp.Choices) > 0 {
			jsonText = openaiResp.Choices[0].Message.Content
		}
	}

	if jsonText == "" {
		return fmt.Errorf("empty response from AI classifier")
	}

	var parsed aiResponse
	if err := json.Unmarshal([]byte(jsonText), &parsed); err != nil {
		return fmt.Errorf("failed to unmarshal AI response: %w", err)
	}

	// Adjust score and confidence with AI insight
	if parsed.IsMatch && parsed.Confidence > listing.Score {
		listing.Score = parsed.Confidence
	} else if !parsed.IsMatch && parsed.Confidence < 40 {
		// Penalty if AI confirmed it's not the match
		listing.Score = (listing.Score + parsed.Confidence) / 2
	}

	listing.MatchReasons = append(listing.MatchReasons, fmt.Sprintf("AI Analysis (%s): %s (confidence %d%%)", c.provider, parsed.Explanation, parsed.Confidence))
	return nil
}
