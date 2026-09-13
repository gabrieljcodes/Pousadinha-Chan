package polymarket

import "bot/internal/locale"

// Outcomes are protocol values shared with the provider and persisted in SQL.
const (
	OutcomeYes = "Yes"
	OutcomeNo  = "No"
)

func OutcomeLabel(outcome string) string {
	switch outcome {
	case OutcomeYes:
		return locale.Text("polymarket.outcome.yes")
	case OutcomeNo:
		return locale.Text("polymarket.outcome.no")
	default:
		return outcome
	}
}
