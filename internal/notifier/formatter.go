package notifier

import (
	"fmt"
	"html"
	"strings"

	"connectclip-finder/internal/model"
)

// FormatTelegramMessage formats a listing into a clean, readable HTML Telegram message.
func FormatTelegramMessage(l *model.Listing) string {
	var sb strings.Builder

	sb.WriteString("🚨 <b>Possible Oticon ConnectClip Found!</b>\n\n")

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

	if len(l.MatchReasons) > 0 {
		topReasons := l.MatchReasons
		if len(topReasons) > 3 {
			topReasons = topReasons[:3]
		}
		sb.WriteString(fmt.Sprintf("🔍 <b>Signals:</b> %s\n", html.EscapeString(strings.Join(topReasons, "; "))))
	}

	if l.URL != "" {
		sb.WriteString(fmt.Sprintf("\n🔗 <a href=\"%s\"><b>Open Listing Link</b></a>", html.EscapeString(l.URL)))
	}

	return sb.String()
}
