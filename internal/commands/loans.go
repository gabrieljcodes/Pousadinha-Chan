package commands

import (
	"crypto/rand"
	"encoding/hex"
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"log"
	"math"
	"strconv"
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

// CmdLoanOffer creates a loan offer for another user
// Usage: !loan offer @user <amount> <interest_rate> <days>
func CmdLoanOffer(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	if len(args) < 5 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Loan System",
			"**Usage:** `!loan offer @user <amount> <interest_rate> <days>`\n\n"+
				"**Parameters:**\n"+
				"• `@user` - The user you want to lend money to\n"+
				"• `amount` - Amount to lend\n"+
				"• `interest_rate` - Interest percentage (e.g., 10 for 10%)\n"+
				"• `days` - Days until payment is due\n\n"+
				"**Example:** `!loan offer @John 1000 10 7`\n"+
				"(Lend 1000 with 10% interest, due in 7 days)"))
		return
	}

	// Check mention
	if len(m.Mentions) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Please mention a user to lend money to."))
		return
	}

	borrower := m.Mentions[0]

	// Parse amount (args[2] because args[0]=offer, args[1]=@user)
	amount, err := strconv.Atoi(args[2])
	if err != nil || amount <= 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid amount. Must be a positive number."))
		return
	}

	// Parse interest rate
	interestRate, err := strconv.ParseFloat(args[3], 64)
	if err != nil || interestRate < 0 || interestRate > 100 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid interest rate. Must be between 0 and 100."))
		return
	}

	// Parse days
	days, err := strconv.Atoi(args[4])
	if err != nil || days <= 0 || days > 365 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Invalid number of days. Must be between 1 and 365."))
		return
	}

	ExecuteLoanOffer(s, m.ChannelID, m.GuildID, m.Author, borrower, amount, interestRate, days, nil)
}

// ExecuteLoanOffer handles loan creation and sends the interactive confirmation message
func ExecuteLoanOffer(s *discordgo.Session, channelID, guildID string, lender *discordgo.User, borrower *discordgo.User, amount int, interestRate float64, days int, i *discordgo.InteractionCreate) {
	// Cannot lend to oneself
	if borrower.ID == lender.ID {
		sendLoanResponse(s, channelID, i, utils.ErrorEmbed("You cannot lend money to yourself!"))
		return
	}

	// Cannot lend to bots
	if borrower.Bot {
		sendLoanResponse(s, channelID, i, utils.ErrorEmbed("You cannot lend money to bots!"))
		return
	}

	// Check if this lender already has an open pending offer for this borrower
	pendingMu.Lock()
	for _, req := range pendingLoans {
		if req.Loan.BorrowerID == borrower.ID && req.Loan.LenderID == lender.ID {
			pendingMu.Unlock()
			sendLoanResponse(s, channelID, i, utils.ErrorEmbed(fmt.Sprintf("You already have an active loan offer pending for <@%s>!", borrower.ID)))
			return
		}
	}
	pendingMu.Unlock()

	// Check lender balance
	lenderBalance := database.GetBalance(lender.ID)
	if lenderBalance < amount {
		sendLoanResponse(s, channelID, i, utils.ErrorEmbed(fmt.Sprintf("Insufficient balance! You have %d %s", lenderBalance, config.Bot.CurrencySymbol)))
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
		Title:       "💰 Loan Offer",
		Description: fmt.Sprintf("<@%s> wants to lend money to <@%s>!", lender.ID, borrower.ID),
		Color:       0xFFD700,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "💵 Amount",
				Value:  fmt.Sprintf("%d %s", amount, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "📈 Interest Rate",
				Value:  fmt.Sprintf("%.1f%% (+%d %s)", interestRate, interest, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "💰 Total to Repay",
				Value:  fmt.Sprintf("%d %s", totalOwed, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "📅 Due Date",
				Value:  fmt.Sprintf("<t:%d:R> (%d days)", dueDate.Unix(), days),
				Inline: true,
			},
			{
				Name:   "🆔 Loan ID",
				Value:  fmt.Sprintf("`%s`", loan.ID),
				Inline: true,
			},
			{
				Name:   "⏱️ Expires In",
				Value:  "60 seconds",
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: "Borrower must click Accept to confirm the loan.",
		},
	}

	// Action buttons
	components := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "Accept",
					Style:    discordgo.SuccessButton,
					CustomID: fmt.Sprintf("loan_accept_%s", loan.ID),
					Emoji:    &discordgo.ComponentEmoji{Name: "✅"},
				},
				discordgo.Button{
					Label:    "Decline",
					Style:    discordgo.DangerButton,
					CustomID: fmt.Sprintf("loan_decline_%s", loan.ID),
					Emoji:    &discordgo.ComponentEmoji{Name: "❌"},
				},
			},
		},
	}

	// If slash command, acknowledge first
	if i != nil {
		_ = s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: fmt.Sprintf("📩 Loan offer sent to <@%s>!", borrower.ID),
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
	}

	msg, err := s.ChannelMessageSendComplex(channelID, &discordgo.MessageSend{
		Content:    fmt.Sprintf("<@%s>, you have received a loan offer from <@%s>!", borrower.ID, lender.ID),
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

func sendLoanResponse(s *discordgo.Session, channelID string, i *discordgo.InteractionCreate, embed *discordgo.MessageEmbed) {
	if i != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{embed},
				Flags:  discordgo.MessageFlagsEphemeral,
			},
		})
	} else {
		s.ChannelMessageSendEmbed(channelID, embed)
	}
}

// CmdLoanPay allows the borrower to pay an active loan
// Usage: !loan pay [loan_id] or !loan pay (pays the earliest active loan)
func CmdLoanPay(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	loanID := ""
	if len(args) >= 2 {
		loanID = strings.TrimSpace(args[1])
	}
	ExecuteLoanPay(s, m.ChannelID, m.Author.ID, loanID, nil)
}

// ExecuteLoanPay executes repayment for an active loan
func ExecuteLoanPay(s *discordgo.Session, channelID, borrowerID, loanID string, i *discordgo.InteractionCreate) {
	userLoans, err := database.GetActiveLoansByBorrower(borrowerID)
	if err != nil || len(userLoans) == 0 {
		sendLoanResponse(s, channelID, i, utils.ErrorEmbed("You don't have any active loans to pay!"))
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
			sendLoanResponse(s, channelID, i, utils.ErrorEmbed("Loan ID not found among your active unpaid loans!"))
			return
		}
	} else {
		// Default to oldest loan
		targetLoanID = userLoans[0].ID
	}

	// Process atomic payment
	paidLoan, err := database.PayLoanAtomic(targetLoanID, borrowerID)
	if err != nil {
		sendLoanResponse(s, channelID, i, utils.ErrorEmbed(fmt.Sprintf("Payment failed: %v", err)))
		return
	}

	successEmbed := utils.SuccessEmbed("Loan Repaid!",
		fmt.Sprintf("🎉 <@%s> paid **%d %s** to <@%s>!\nLoan `%s` is now fully settled.",
			paidLoan.BorrowerID, paidLoan.TotalOwed, config.Bot.CurrencySymbol, paidLoan.LenderID, paidLoan.ID))

	if i != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{successEmbed},
			},
		})
	} else {
		s.ChannelMessageSendEmbed(channelID, successEmbed)
	}
}

// CmdLoanList lists active loans for the user
// Usage: !loan list or !loan list @user
func CmdLoanList(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	targetUser := m.Author
	isOwn := true

	if len(m.Mentions) > 0 {
		targetUser = m.Mentions[0]
		isOwn = false
	}

	ExecuteLoanList(s, m.ChannelID, targetUser, isOwn, nil)
}

// ExecuteLoanList lists active loans directly from PostgreSQL
func ExecuteLoanList(s *discordgo.Session, channelID string, targetUser *discordgo.User, isOwn bool, i *discordgo.InteractionCreate) {
	userLoans, err := database.GetActiveLoansByUser(targetUser.ID, 15)
	if err != nil || len(userLoans) == 0 {
		msg := "You don't have any active loans!"
		if !isOwn {
			msg = fmt.Sprintf("%s doesn't have any active loans!", targetUser.Username)
		}
		sendLoanResponse(s, channelID, i, utils.InfoEmbed("Loans", msg))
		return
	}

	var description strings.Builder
	description.WriteString(fmt.Sprintf("**Active Loans for %s**\n\n", targetUser.Username))

	for idx, loan := range userLoans {
		role := "Borrower"
		otherParty := loan.LenderID
		if loan.LenderID == targetUser.ID {
			role = "Lender"
			otherParty = loan.BorrowerID
		}

		timeLeft := time.Until(loan.DueDate)
		statusEmoji := "🟢"
		if timeLeft < 24*time.Hour {
			statusEmoji = "🟡"
		}
		if timeLeft < 0 {
			statusEmoji = "🔴 OVERDUE"
		}

		description.WriteString(fmt.Sprintf(
			"**%d.** `%s`\n"+
				"• Role: **%s** | Other: <@%s>\n"+
				"• Borrowed: **%d %s** | Debt: **%d %s**\n"+
				"• Due: %s (%s)\n\n",
			idx+1, loan.ID, // Full un-truncated ID in code block
			role, otherParty,
			loan.Amount, config.Bot.CurrencySymbol, loan.TotalOwed, config.Bot.CurrencySymbol,
			statusEmoji, formatDuration(timeLeft),
		))
	}

	description.WriteString("💡 *To pay a loan, use `!loan pay <id>` or `/loan pay loan_id:<id>`*")
	embed := utils.GoldEmbed("📋 Active Loans", description.String())

	if i != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Embeds: []*discordgo.MessageEmbed{embed},
			},
		})
	} else {
		s.ChannelMessageSendEmbed(channelID, embed)
	}
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
				Content: "❌ This loan offer has expired or does not exist!",
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
				Content: "❌ This loan offer is not for you!",
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
				Content:    fmt.Sprintf("❌ Loan could not be completed: lender may no longer have sufficient balance (%v)", err),
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
			Content: fmt.Sprintf("✅ **Loan Accepted!**\n<@%s> received **%d %s** from <@%s>.\nTotal to repay: **%d %s** by <t:%d:f>\nLoan ID: `%s`",
				loan.BorrowerID, loan.Amount, config.Bot.CurrencySymbol, loan.LenderID,
				loan.TotalOwed, config.Bot.CurrencySymbol, loan.DueDate.Unix(), loan.ID),
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
				Content: "❌ This loan offer has expired or does not exist!",
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
				Content: "❌ You cannot decline this loan offer.",
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
			Content:    fmt.Sprintf("❌ <@%s> %s the loan offer.", userID, actionText),
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

	content := fmt.Sprintf("⏰ **Loan offer expired!** <@%s> did not respond in time.", borrowerID)
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
	log.Println("Starting background loan overdue collector worker (1-minute interval)...")
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
			embed := utils.SuccessEmbed("Auto Payment Executed",
				fmt.Sprintf("💰 **Loan auto-collected!**\n<@%s> paid **%d %s** to <@%s>.\nLoan `%s` is now fully repaid! ✅",
					updatedLoan.BorrowerID, collected, config.Bot.CurrencySymbol, updatedLoan.LenderID, updatedLoan.ID))
			_, _ = s.ChannelMessageSendEmbed(loan.ChannelID, embed)
		} else if collected > 0 {
			embed := utils.GoldEmbed("⚠️ Partial Loan Collection",
				fmt.Sprintf("⚠️ <@%s> did not have enough funds to repay loan `%s` in full!\n"+
					"• Collected: **%d %s**\n"+
					"• Remaining Debt: **%d %s**\n"+
					"The loan remains **OVERDUE** until fully settled. 💸",
					updatedLoan.BorrowerID, updatedLoan.ID, collected, config.Bot.CurrencySymbol, remaining, config.Bot.CurrencySymbol))
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
		return fmt.Sprintf("%d days overdue", int(d.Hours()/24))
	}

	days := int(d.Hours() / 24)
	hours := int(d.Hours()) % 24

	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	return fmt.Sprintf("%dh", hours)
}
