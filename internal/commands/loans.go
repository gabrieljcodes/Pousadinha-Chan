package commands

import (
	"bot/internal/database"
	"bot/internal/locale"
	"bot/pkg/config"
	"bot/pkg/utils"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"math"

	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// PendingLoanRequest represents a pending loan request
type PendingLoanRequest struct {
	Loan      *database.Loan
	Timeout   *time.Timer
	MessageID string
}

var (
	// pendingLoans stores pending loan requests: loanID -> PendingLoanRequest
	pendingLoans = make(map[string]*PendingLoanRequest)
	pendingMu    sync.Mutex
)

// generateLoanID generates a short, unique, collision-proof ID for the loan
func generateLoanID() string {
	b := make([]byte, 3)
	_, _ = rand.Read(b)
	return fmt.Sprintf("ln_%d_%s", time.Now().Unix()%1000000, hex.EncodeToString(b))
}

// ExecuteLoanOffer handles loan creation and sends the interactive confirmation message
func ExecuteLoanOffer(s *discordgo.Session, channelID, guildID string, lender *discordgo.User, borrower *discordgo.User, amount int, interestRate float64, days int, i *discordgo.InteractionCreate) {
	// Cannot lend to oneself
	if borrower.ID == lender.ID {
		sendLoanResponse(s, i, utils.ErrorEmbed(locale.Text("commands.loans.you_cannot_lend_money_to_yourself")))
		return
	}

	// Cannot lend to bots
	if borrower.Bot {
		sendLoanResponse(s, i, utils.ErrorEmbed(locale.Text("commands.loans.you_cannot_lend_money_to_bots")))
		return
	}

	// Check if this lender already has an open pending offer for this borrower
	pendingMu.Lock()
	for _, req := range pendingLoans {
		if req.Loan.BorrowerID == borrower.ID && req.Loan.LenderID == lender.ID {
			pendingMu.Unlock()
			sendLoanResponse(s, i, utils.ErrorEmbed(locale.Text("commands.loans.you_already_have_an_active_loan_offer.formatted", locale.Data{"ID": borrower.ID})))
			return
		}
	}
	pendingMu.Unlock()

	// Check lender balance
	lenderBalance := database.GetBalance(guildID, lender.ID)
	if lenderBalance < amount {
		sendLoanResponse(s, i, utils.ErrorEmbed(locale.Text("commands.loans.insufficient_balance_you_have.formatted", locale.Data{"LenderBalance": lenderBalance, "CurrencySymbol": config.Bot.CurrencySymbol})))
		return
	}

	// Calculate total amount with proper rounding and minimum 1 coin interest if rate > 0
	interest := int(math.Round(float64(amount) * (interestRate / 100.0)))
	if interestRate > 0 && interest == 0 {
		interest = 1
	}
	totalOwed := amount + interest
	dueDate := time.Now().Add(time.Duration(days) * 24 * time.Hour)

	// Create the loan model
	loan := &database.Loan{
		ID:           generateLoanID(),
		LenderID:     lender.ID,
		BorrowerID:   borrower.ID,
		Amount:       amount,
		InterestRate: interestRate,
		DueDate:      dueDate,
		TotalOwed:    totalOwed,
		Paid:         false,
		CreatedAt:    time.Now(),
		ChannelID:    channelID,
		GuildID:      guildID,
	}

	// Confirmation embed
	embed := &discordgo.MessageEmbed{
		Title:       locale.Text("commands.loans.loan_offer"),
		Description: locale.Text("commands.loans.wants_to_lend_money_to.formatted", locale.Data{"ID": lender.ID, "ID2": borrower.ID}),
		Color:       0xFFD700,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   locale.Text("commands.loans.amount"),
				Value:  fmt.Sprintf("%d %s", amount, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   locale.Text("commands.loans.interest_rate"),
				Value:  fmt.Sprintf("%.1f%% (+%d %s)", interestRate, interest, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   locale.Text("commands.loans.total_to_repay"),
				Value:  fmt.Sprintf("%d %s", totalOwed, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   locale.Text("commands.loans.due_date"),
				Value:  locale.Text("commands.loans.t_r_days.formatted", locale.Data{"DueDate": dueDate.Unix(), "Days": days}),
				Inline: true,
			},
			{
				Name:   locale.Text("commands.loans.loan_id"),
				Value:  fmt.Sprintf("`%s`", loan.ID),
				Inline: true,
			},
			{
				Name:   locale.Text("commands.loans.expires_in"),
				Value:  locale.Text("commands.loans.seconds"),
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: locale.Text("commands.loans.borrower_must_click_accept_to_confirm_the"),
		},
	}

	// Action buttons
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    locale.Text("commands.loans.accept"),
					Style:    discordgo.SuccessButton,
					CustomID: fmt.Sprintf("loan_accept_%s", loan.ID),
					Emoji:    &discordgo.ComponentEmoji{Name: "✅"},
				},
				discordgo.Button{
					Label:    locale.Text("commands.loans.decline"),
					Style:    discordgo.DangerButton,
					CustomID: fmt.Sprintf("loan_decline_%s", loan.ID),
					Emoji:    &discordgo.ComponentEmoji{Name: "❌"},
				},
			},
		},
	}

	// Acknowledge before publishing the public offer.

	_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Content: locale.Text("commands.loans.loan_offer_sent_to.formatted", locale.Data{"ID": borrower.ID}),
			Flags:   discordgo.MessageFlagsEphemeral,
		},
	})

	msg, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    locale.Text("commands.loans.you_have_received_a_loan_offer_from.formatted", locale.Data{"ID": borrower.ID, "ID2": lender.ID}),
		Embed:      embed,
		Components: components,
	})
	if err != nil {
		log.Printf("[Loan Offer Error] Failed to send message: %v", err)
		return
	}

	// 60-second expiration timer
	timeout := time.AfterFunc(60*time.Second, func() {
		expireLoanOffer(s, loan.ID, channelID, msg.ID)
	})

	pendingMu.Lock()
	pendingLoans[loan.ID] = &PendingLoanRequest{
		Loan:      loan,
		Timeout:   timeout,
		MessageID: msg.ID,
	}
	pendingMu.Unlock()
}

func sendLoanResponse(s *discordgo.Session, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
			Flags:  discordgo.MessageFlagsEphemeral,
		},
	})

}

// ExecuteLoanPay executes repayment for an active loan
func ExecuteLoanPay(s *discordgo.Session, borrowerID, loanID string, i *discordgo.InteractionCreate) {
	userLoans, err := database.GetActiveLoansByBorrower(borrowerID)
	if err != nil || len(userLoans) == 0 {
		sendLoanResponse(s, i, utils.ErrorEmbed(locale.Text("commands.loans.you_don_t_have_any_active_loans")))
		return
	}

	var targetLoanID string
	if loanID != "" {
		// Find specific loan
		for _, l := range userLoans {
			if l.ID == loanID {
				targetLoanID = l.ID
				break
			}
		}
		if targetLoanID == "" {
			sendLoanResponse(s, i, utils.ErrorEmbed(locale.Text("commands.loans.loan_id_not_found_among_your_active")))
			return
		}
	} else {
		// Default to oldest loan
		targetLoanID = userLoans[0].ID
	}

	// Process atomic payment
	paidLoan, err := database.PayLoanAtomic(targetLoanID, borrowerID)
	if err != nil {
		sendLoanResponse(s, i, utils.ErrorEmbed(locale.Text("commands.loans.payment_failed.formatted", locale.Data{"Err": err})))
		return
	}

	successEmbed := utils.SuccessEmbed(locale.Text("commands.loans.loan_repaid"),
		locale.Text("commands.loans.paid_to_loan_is_now_fully_settled.formatted", locale.Data{"BorrowerID": paidLoan.BorrowerID, "TotalOwed": paidLoan.TotalOwed, "CurrencySymbol": config.Bot.CurrencySymbol, "LenderID": paidLoan.LenderID, "ID": paidLoan.ID}))

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{successEmbed},
		},
	})

}

// ExecuteLoanList lists active loans directly from PostgreSQL
func ExecuteLoanList(s *discordgo.Session, targetUser *discordgo.User, isOwn bool, i *discordgo.InteractionCreate) {
	userLoans, err := database.GetActiveLoansByUser(targetUser.ID, 15)
	if err != nil || len(userLoans) == 0 {
		msg := locale.Text("commands.loans.you_don_t_have_any_active_loans_4c56a6")
		if !isOwn {
			msg = locale.Text("commands.loans.doesn_t_have_any_active_loans.formatted", locale.Data{"Username": targetUser.Username})
		}
		sendLoanResponse(s, i, utils.InfoEmbed(locale.Text("commands.general.loans"), msg))
		return
	}

	var description strings.Builder
	description.WriteString(locale.Text("commands.loans.active_loans_for.formatted", locale.Data{"Username": targetUser.Username}))

	for idx, loan := range userLoans {
		role := locale.Text("commands.loans.borrower")
		otherParty := loan.LenderID
		if loan.LenderID == targetUser.ID {
			role = locale.Text("commands.loans.lender")
			otherParty = loan.BorrowerID
		}

		timeLeft := time.Until(loan.DueDate)
		statusEmoji := "🟢"
		if timeLeft < 24*time.Hour {
			statusEmoji = "🟡"
		}
		if timeLeft < 0 {
			statusEmoji = locale.Text("commands.loans.overdue")
		}

		description.WriteString(locale.Text("commands.loans.role_other_borrowed_debt_due.formatted", locale.Data{"Idx": idx + 1, "ID": loan.ID, "Role": role, "OtherParty": otherParty, "Amount": loan.Amount, "CurrencySymbol": config.Bot.CurrencySymbol, "TotalOwed": loan.TotalOwed, "CurrencySymbol8": config.Bot.CurrencySymbol, "StatusEmoji": statusEmoji, "Value10": formatDuration(timeLeft)}))
	}

	description.WriteString(locale.Text("commands.loans.to_pay_a_loan_use_loan_pay"))
	embed := utils.GoldEmbed(locale.Text("commands.loans.active_loans"), description.String())

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseChannelMessageWithSource,
		Data: &discordgo.InteractionResponseData{
			Embeds: []*discordgo.MessageEmbed{embed},
		},
	})

}

// HandleLoanAccept accepts a loan offer atomically
func HandleLoanAccept(s *discordgo.Session, i *discordgo.InteractionCreate, loanID string) {
	userID := i.Member.User.ID

	pendingMu.Lock()
	request, exists := pendingLoans[loanID]
	if !exists {
		pendingMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.loans.this_loan_offer_has_expired_or_does"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Verify designated borrower
	if userID != request.Loan.BorrowerID {
		pendingMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.loans.this_loan_offer_is_not_for_you"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	request.Timeout.Stop()
	delete(pendingLoans, loanID)
	pendingMu.Unlock()

	loan := request.Loan

	// Atomic transaction: locks lender balance, transfers money, and inserts loan record
	err := database.AcceptLoanAtomic(loan)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    locale.Text("commands.loans.loan_could_not_be_completed_lender_may.formatted", locale.Data{"Err": err}),
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	// Update message
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    locale.Text("commands.loans.loan_accepted_received_from_total_to_repay.formatted", locale.Data{"BorrowerID": loan.BorrowerID, "Amount": loan.Amount, "CurrencySymbol": config.Bot.CurrencySymbol, "LenderID": loan.LenderID, "TotalOwed": loan.TotalOwed, "CurrencySymbol6": config.Bot.CurrencySymbol, "DueDate": loan.DueDate.Unix(), "ID": loan.ID}),
			Embeds:     []*discordgo.MessageEmbed{},
			Components: []discordgo.MessageComponent{},
		},
	})
}

// HandleLoanDecline declines or cancels a loan offer
func HandleLoanDecline(s *discordgo.Session, i *discordgo.InteractionCreate, loanID string) {
	userID := i.Member.User.ID

	pendingMu.Lock()
	request, exists := pendingLoans[loanID]
	if !exists {
		pendingMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.loans.this_loan_offer_has_expired_or_does"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	if userID != request.Loan.BorrowerID && userID != request.Loan.LenderID {
		pendingMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: locale.Text("commands.loans.you_cannot_decline_this_loan_offer"),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	request.Timeout.Stop()
	delete(pendingLoans, loanID)
	pendingMu.Unlock()

	actionText := "declined"
	if userID == request.Loan.LenderID {
		actionText = "cancelled"
	}

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    locale.Text("commands.loans.the_loan_offer.formatted", locale.Data{"UserID": userID, "ActionText": actionText}),
			Embeds:     []*discordgo.MessageEmbed{},
			Components: []discordgo.MessageComponent{},
		},
	})
}

// expireLoanOffer expires a loan offer after timeout
func expireLoanOffer(s *discordgo.Session, loanID, channelID, messageID string) {
	pendingMu.Lock()
	req, exists := pendingLoans[loanID]
	if !exists {
		pendingMu.Unlock()
		return
	}
	delete(pendingLoans, loanID)
	borrowerID := req.Loan.BorrowerID
	pendingMu.Unlock()

	content := locale.Text("commands.loans.loan_offer_expired_did_not_respond_in.formatted", locale.Data{"BorrowerID": borrowerID})
	embeds := []*discordgo.MessageEmbed{}
	components := []discordgo.MessageComponent{}
	_, _ = s.ChannelMessageEditComplex(&discordgo.MessageEdit{
		Channel:    channelID,
		ID:         messageID,
		Content:    &content,
		Embeds:     &embeds,
		Components: &components,
	})
}

// StartLoanWorker starts the background ticker that periodically processes overdue loans.
func StartLoanWorker(s *discordgo.Session) {
	log.Println(locale.Text("commands.loans.starting_background_loan_overdue_collector_worker_minute"))
	ticker := time.NewTicker(1 * time.Minute)

	go func() {
		// Run once on startup
		processOverdueLoans(s)
		for range ticker.C {
			processOverdueLoans(s)
		}
	}()
}

// processOverdueLoans queries PostgreSQL for overdue loans and auto-collects funds atomically
func processOverdueLoans(s *discordgo.Session) {
	overdueLoans, err := database.GetOverdueUnpaidLoans()
	if err != nil || len(overdueLoans) == 0 {
		return
	}

	for _, loan := range overdueLoans {
		updatedLoan, collected, remaining, fullyPaid, err := database.AutoCollectDueLoan(loan.ID)
		if err != nil {
			log.Printf("[Loan Worker] Error auto-collecting loan %s: %v", loan.ID, err)
			continue
		}

		if fullyPaid {
			embed := utils.SuccessEmbed(locale.Text("commands.loans.auto_payment_executed"),
				locale.Text("commands.loans.loan_auto_collected_paid_to_loan_is.formatted", locale.Data{"BorrowerID": updatedLoan.BorrowerID, "Collected": collected, "CurrencySymbol": config.Bot.CurrencySymbol, "LenderID": updatedLoan.LenderID, "ID": updatedLoan.ID}))
			_, _ = s.ChannelMessageSendEmbed(loan.ChannelID, embed)
		} else if collected > 0 {
			embed := utils.GoldEmbed(locale.Text("commands.loans.partial_loan_collection"),
				locale.Text("commands.loans.did_not_have_enough_funds_to_repay.formatted", locale.Data{"BorrowerID": updatedLoan.BorrowerID, "ID": updatedLoan.ID, "Collected": collected, "CurrencySymbol": config.Bot.CurrencySymbol, "Remaining": remaining, "CurrencySymbol6": config.Bot.CurrencySymbol}))
			_, _ = s.ChannelMessageSendEmbed(loan.ChannelID, embed)
		}
	}
}

// LoadActiveLoans is kept for backward-compatibility with main.go startup calls
func LoadActiveLoans(s *discordgo.Session) {
	StartLoanWorker(s)
}

// formatDuration formats duration for display
func formatDuration(d time.Duration) string {
	if d < 0 {
		d = -d
		return locale.Text("commands.loans.days_overdue.formatted", locale.Data{"Value1": int(d.Hours() / 24)})
	}

	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dh", hours)
}
