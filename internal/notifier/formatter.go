package notifier

import (
	"fmt"
	"html"
	"strings"

	"connectclip-finder/internal/model"
)

// FormatTelegramMessage formats a listing into a clean, readable HTML Telegram message.
func FormatTelegramMessage(l *model.Listing) string {
	return FormatTelegramMessageWithTarget(l, "Oticon ConnectClip")
}

// FormatTelegramMessageWithTarget formats a listing into a clean, readable HTML Telegram message with dynamic target name.
func FormatTelegramMessageWithTarget(l *model.Listing, targetName string) string {
	if targetName == "" {
		targetName = "Oticon ConnectClip"
	}
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("🚨 <b>Possible %s Found!</b>\n\n", html.EscapeString(targetName)))

	sb.WriteString(fmt.Sprintf("🏷️ <b>Source:</b> %s\n", html.EscapeString(string(l.Source))))
	sb.WriteString(fmt.Sprintf("📋 <b>Title:</b> %s\n", html.EscapeString(l.Title)))

	if l.Price > 0 {
		currency := l.Currency
		if currency == "" {
			currency = "EUR"
		}
		sb.WriteString(fmt.Sprintf("💰 <b>Price:</b> €%.2f %s\n", l.Price, html.EscapeString(currency)))
	} else {
		sb.WriteString("💰 <b>Price:</b> Not specified / Contact seller\n")
	}

	if l.Location != "" {
		sb.WriteString(fmt.Sprintf("📍 <b>Location:</b> %s\n", html.EscapeString(l.Location)))
	}

	if l.Seller != "" {
		sb.WriteString(fmt.Sprintf("👤 <b>Seller:</b> %s\n", html.EscapeString(l.Seller)))
	}

	sb.WriteString(fmt.Sprintf("🎯 <b>Confidence:</b> %d%% (%s)\n", l.Score, l.Confidence))

	var alertReason string
	var signals []string
	for _, r := range l.MatchReasons {
		if strings.HasPrefix(r, "Alert rule met:") || strings.HasPrefix(r, "Match score") {
			if alertReason == "" {
				alertReason = r
			}
		} else {
			signals = append(signals, r)
		}
	}

	if alertReason != "" {
		sb.WriteString(fmt.Sprintf("⚡ <b>Alert Reason:</b> %s\n", html.EscapeString(alertReason)))
	}

	if len(signals) > 0 {
		if len(signals) > 3 {
			signals = signals[:3]
		}
		sb.WriteString(fmt.Sprintf("🔍 <b>Signals:</b> %s\n", html.EscapeString(strings.Join(signals, "; "))))
	}

	if l.URL != "" {
		sb.WriteString(fmt.Sprintf("\n🔗 <a href=\"%s\"><b>Open Listing Link</b></a>", html.EscapeString(l.URL)))
	}

	return sb.String()
}
