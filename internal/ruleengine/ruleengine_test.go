package ruleengine

import (
	"testing"
)

func TestRuleEngine(t *testing.T) {
	ctx := Context{
		Price:          350.0,
		Score:          85.0,
		TargetMinPrice: 200.0,
		TargetMaxPrice: 450.0,
		MaxAlertPrice:  400.0,
		AlertThreshold: 70.0,
		HasPhoto:       true,
		Source:         "ss.com",
	}

	tests := []struct {
		name     string
		expr     string
		expected bool
	}{
		{
			name:     "Great deal preset passes",
			expr:     ResolvePresetEquation(PresetGreatDeal, ctx.TargetMaxPrice, ctx.AlertThreshold),
			expected: true,
		},
		{
			name:     "Steal deal fails because 350 is not <= 450 * 0.75 (337.50)",
			expr:     ResolvePresetEquation(PresetStealDeal, ctx.TargetMaxPrice, ctx.AlertThreshold),
			expected: false,
		},
		{
			name:     "High match with photo passes",
			expr:     ResolvePresetEquation(PresetHighMatchPhoto, ctx.TargetMaxPrice, ctx.AlertThreshold),
			expected: true,
		},
		{
			name:     "Custom equation with price and score",
			expr:     "price <= 400 && score >= 75",
			expected: true,
		},
		{
			name:     "Custom equation with parentheses and source match",
			expr:     "(source == 'ss.com' || source == 'banknote') && price < 500",
			expected: true,
		},
		{
			name:     "Custom equation failing on price",
			expr:     "price < 300",
			expected: false,
		},
		{
			name:     "Custom equation with arithmetic discount",
			expr:     "price <= (target_max_price - 50) && has_photo == 1",
			expected: true,
		},
		{
			name:     "Empty expression defaults to score >= threshold and max_alert_price check",
			expr:     "",
			expected: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			passed, reason, err := Evaluate(tc.expr, ctx)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}
			if passed != tc.expected {
				t.Errorf("Expected %v, got %v (reason: %s)", tc.expected, passed, reason)
			}
		})
	}
}
