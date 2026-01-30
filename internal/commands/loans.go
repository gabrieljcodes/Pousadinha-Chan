package commands

import (
	"estudocoin/internal/database"
	"estudocoin/pkg/config"
	"estudocoin/pkg/utils"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/bwmarrin/discordgo"
)

// PendingLoanRequest representa uma solicitação de empréstimo pendente
type PendingLoanRequest struct {
	Loan      *database.Loan
	Timeout   *time.Timer
	MessageID string
}

var (
	// loans armazena todos os empréstimos ativos: loanID -> Loan
	loans = make(map[string]*database.Loan)
	loansMu sync.RWMutex

	// pendingLoans armazena solicitações pendentes: borrowerID -> PendingLoanRequest
	pendingLoans = make(map[string]*PendingLoanRequest)
	pendingMu    sync.Mutex

	// loanIDCounter para gerar IDs únicos
	loanIDCounter int64
	loanIDMu      sync.Mutex
)

// generateLoanID gera um ID único para o empréstimo
func generateLoanID() string {
	loanIDMu.Lock()
	defer loanIDMu.Unlock()
	loanIDCounter++
	return fmt.Sprintf("loan_%d_%d", time.Now().Unix(), loanIDCounter)
}

// CmdLoanOffer cria uma oferta de empréstimo para outro usuário
// Uso: !loan offer @user <amount> <interest_rate> <days>
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

	// Verificar menção
	if len(m.Mentions) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Please mention a user to lend money to."))
		return
	}

	borrower := m.Mentions[0]
	lenderID := m.Author.ID

	// Não pode emprestar para si mesmo
	if borrower.ID == lenderID {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You cannot lend money to yourself!"))
		return
	}

	// Não pode emprestar para bots
	if borrower.Bot {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You cannot lend money to bots!"))
		return
	}

	// Parse amount (args[2] porque args[0]=offer, args[1]=@usuario)
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

	// Verificar se o usuário já tem uma solicitação pendente
	pendingMu.Lock()
	if _, exists := pendingLoans[borrower.ID]; exists {
		pendingMu.Unlock()
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("<@%s> already has a pending loan request!", borrower.ID)))
		return
	}
	pendingMu.Unlock()

	// Verificar saldo do credor
	lenderBalance := database.GetBalance(lenderID)
	if lenderBalance < amount {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(fmt.Sprintf("Insufficient balance! You have %d %s", lenderBalance, config.Bot.CurrencySymbol)))
		return
	}

	// Calcular valor total
	interest := int(float64(amount) * (interestRate / 100))
	totalOwed := amount + interest
	dueDate := time.Now().Add(time.Duration(days) * 24 * time.Hour)

	// Criar o empréstimo
	loan := &database.Loan{
		ID:           generateLoanID(),
		LenderID:     lenderID,
		BorrowerID:   borrower.ID,
		Amount:       amount,
		InterestRate: interestRate,
		DueDate:      dueDate,
		TotalOwed:    totalOwed,
		Paid:         false,
		CreatedAt:    time.Now(),
		ChannelID:    m.ChannelID,
		GuildID:      m.GuildID,
	}

	// Criar mensagem de confirmação
	embed := &discordgo.MessageEmbed{
		Title:       "💰 Loan Offer",
		Description: fmt.Sprintf("<@%s> wants to lend money to <@%s>!", lenderID, borrower.ID),
		Color:       0xFFD700,
		Fields: []*discordgo.MessageEmbedField{
			{
				Name:   "💵 Amount",
				Value:  fmt.Sprintf("%d %s", amount, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "📈 Interest Rate",
				Value:  fmt.Sprintf("%.1f%%", interestRate),
				Inline: true,
			},
			{
				Name:   "💸 Total to Pay",
				Value:  fmt.Sprintf("%d %s", totalOwed, config.Bot.CurrencySymbol),
				Inline: true,
			},
			{
				Name:   "📅 Due Date",
				Value:  fmt.Sprintf("<t:%d:f>", dueDate.Unix()),
				Inline: true,
			},
			{
				Name:   "⏱️ Time to Accept",
				Value:  "1 minute",
				Inline: true,
			},
		},
		Footer: &discordgo.MessageEmbedFooter{
			Text: fmt.Sprintf("Loan ID: %s", loan.ID),
		},
	}

	buttons := []discordgo.MessageComponent{
		discordgo.ActionsRow{
			Components: []discordgo.MessageComponent{
				discordgo.Button{
					Label:    "✅ Accept",
					Style:    discordgo.SuccessButton,
					CustomID: fmt.Sprintf("loan_accept_%s", loan.ID),
				},
				discordgo.Button{
					Label:    "❌ Decline",
					Style:    discordgo.DangerButton,
					CustomID: fmt.Sprintf("loan_decline_%s", loan.ID),
				},
			},
		},
	}

	msg, err := s.ChannelMessageSendComplex(m.ChannelID, &discordgo.MessageSend{
		Embeds:     []*discordgo.MessageEmbed{embed},
		Components: buttons,
	})

	if err != nil {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Error creating loan offer."))
		return
	}

	// Configurar timeout de 1 minuto
	timeout := time.AfterFunc(1*time.Minute, func() {
		expireLoanOffer(s, loan.ID, m.ChannelID, msg.ID)
	})

	// Armazenar solicitação pendente
	pendingMu.Lock()
	pendingLoans[borrower.ID] = &PendingLoanRequest{
		Loan:      loan,
		Timeout:   timeout,
		MessageID: msg.ID,
	}
	pendingMu.Unlock()
}

// CmdLoanPay permite ao devedor pagar um empréstimo
// Uso: !loan pay [loan_id] ou !loan pay (paga o primeiro empréstimo ativo)
func CmdLoanPay(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	borrowerID := m.Author.ID

	// Buscar empréstimos ativos do usuário
	loansMu.RLock()
	var userLoans []*database.Loan
	for _, loan := range loans {
		if loan.BorrowerID == borrowerID && !loan.Paid {
			userLoans = append(userLoans, loan)
		}
	}
	loansMu.RUnlock()

	if len(userLoans) == 0 {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("You don't have any active loans to pay!"))
		return
	}

	var loanToPay *database.Loan

	// Se especificou um ID, procurar por ele
	if len(args) >= 2 {
		loanID := args[1]
		for _, loan := range userLoans {
			if loan.ID == loanID {
				loanToPay = loan
				break
			}
		}
		if loanToPay == nil {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed("Loan not found or already paid!"))
			return
		}
	} else {
		// Pega o empréstimo mais antigo (primeiro da lista)
		loanToPay = userLoans[0]
	}

	// Verificar saldo
	balance := database.GetBalance(borrowerID)
	if balance < loanToPay.TotalOwed {
		s.ChannelMessageSendEmbed(m.ChannelID, utils.ErrorEmbed(
			fmt.Sprintf("Insufficient balance! You need %d %s but have %d %s.",
				loanToPay.TotalOwed, config.Bot.CurrencySymbol, balance, config.Bot.CurrencySymbol)))
		return
	}

	// Realizar pagamento
	processLoanPayment(s, m.ChannelID, loanToPay, borrowerID)
}

// CmdLoanList lista todos os empréstimos ativos do usuário
// Uso: !loan list ou !loan list @user (para ver de outro usuário)
func CmdLoanList(s *discordgo.Session, m *discordgo.MessageCreate, args []string) {
	targetID := m.Author.ID
	targetName := m.Author.Username
	isOwn := true

	// Se mencionou alguém, mostra os empréstimos dele
	if len(m.Mentions) > 0 {
		targetID = m.Mentions[0].ID
		targetName = m.Mentions[0].Username
		isOwn = false
	}

	loansMu.RLock()
	var userLoans []*database.Loan
	for _, loan := range loans {
		if (loan.BorrowerID == targetID || loan.LenderID == targetID) && !loan.Paid {
			userLoans = append(userLoans, loan)
		}
	}
	loansMu.RUnlock()

	if len(userLoans) == 0 {
		if isOwn {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Loans", "You don't have any active loans!"))
		} else {
			s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("Loans", fmt.Sprintf("%s doesn't have any active loans!", targetName)))
		}
		return
	}

	// Construir a lista
	var description strings.Builder
	description.WriteString(fmt.Sprintf("**Active Loans for %s**\n\n", targetName))

	for i, loan := range userLoans {
		role := "Borrower"
		otherParty := loan.LenderID
		if loan.LenderID == targetID {
			role = "Lender"
			otherParty = loan.BorrowerID
		}

		timeLeft := loan.DueDate.Sub(time.Now())
		statusEmoji := "🟢"
		if timeLeft < 24*time.Hour {
			statusEmoji = "🟡"
		}
		if timeLeft < 0 {
			statusEmoji = "🔴 OVERDUE"
		}

		// Truncar ID de forma segura
		idDisplay := loan.ID
		if len(idDisplay) > 20 {
			idDisplay = idDisplay[:20] + "..."
		}

		description.WriteString(fmt.Sprintf(
			"**%d.** `%s`\n"+
			"Role: %s | Other: <@%s>\n"+
			"Amount: %d %s | Total: %d %s\n"+
			"Due: %s %s\n\n",
			i+1, idDisplay,
			role, otherParty,
			loan.Amount, config.Bot.CurrencySymbol, loan.TotalOwed, config.Bot.CurrencySymbol,
			statusEmoji, formatDuration(timeLeft),
		))
	}

	s.ChannelMessageSendEmbed(m.ChannelID, utils.InfoEmbed("📋 Active Loans", description.String()))
}

// HandleLoanAccept aceita uma oferta de empréstimo
func HandleLoanAccept(s *discordgo.Session, i *discordgo.InteractionCreate, loanID string) {
	userID := i.Member.User.ID

	pendingMu.Lock()
	request, exists := pendingLoans[userID]
	if !exists || request.Loan.ID != loanID {
		pendingMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This loan offer has expired or is invalid!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	// Cancelar timeout
	request.Timeout.Stop()
	delete(pendingLoans, userID)
	pendingMu.Unlock()

	loan := request.Loan

	// Verificar se o credor ainda tem saldo
	lenderBalance := database.GetBalance(loan.LenderID)
	if lenderBalance < loan.Amount {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    fmt.Sprintf("❌ <@%s> no longer has sufficient balance!", loan.LenderID),
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	// Transferir dinheiro
	err := database.TransferCoins(loan.LenderID, loan.BorrowerID, loan.Amount)
	if err != nil {
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "❌ Error processing loan transaction!",
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	// Salvar empréstimo no banco
	err = database.SaveLoan(loan)
	if err != nil {
		// Tentar reverter a transferência
		database.TransferCoins(loan.BorrowerID, loan.LenderID, loan.Amount)
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseUpdateMessage,
			Data: &discordgo.InteractionResponseData{
				Content:    "❌ Error saving loan to database!",
				Embeds:     []*discordgo.MessageEmbed{},
				Components: []discordgo.MessageComponent{},
			},
		})
		return
	}

	// Adicionar à lista de empréstimos ativos
	loansMu.Lock()
	loans[loan.ID] = loan
	loansMu.Unlock()

	// Agendar cobrança automática
	scheduleAutoCollection(s, loan)

	// Atualizar mensagem
	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content: fmt.Sprintf("✅ **Loan Accepted!**\n<@%s> received **%d %s** from <@%s>.\nTotal to pay: **%d %s** by <t:%d:f>",
				loan.BorrowerID, loan.Amount, config.Bot.CurrencySymbol, loan.LenderID,
				loan.TotalOwed, config.Bot.CurrencySymbol, loan.DueDate.Unix()),
			Embeds:     []*discordgo.MessageEmbed{},
			Components: []discordgo.MessageComponent{},
		},
	})
}

// HandleLoanDecline recusa uma oferta de empréstimo
func HandleLoanDecline(s *discordgo.Session, i *discordgo.InteractionCreate, loanID string) {
	userID := i.Member.User.ID

	pendingMu.Lock()
	request, exists := pendingLoans[userID]
	if !exists || request.Loan.ID != loanID {
		pendingMu.Unlock()
		s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
			Type: discordgo.InteractionResponseChannelMessageWithSource,
			Data: &discordgo.InteractionResponseData{
				Content: "❌ This loan offer has expired or is invalid!",
				Flags:   discordgo.MessageFlagsEphemeral,
			},
		})
		return
	}

	request.Timeout.Stop()
	delete(pendingLoans, userID)
	pendingMu.Unlock()

	s.InteractionRespond(i.Interaction, &discordgo.InteractionResponse{
		Type: discordgo.InteractionResponseUpdateMessage,
		Data: &discordgo.InteractionResponseData{
			Content:    fmt.Sprintf("❌ <@%s> declined the loan offer.", userID),
			Embeds:     []*discordgo.MessageEmbed{},
			Components: []discordgo.MessageComponent{},
		},
	})
}

// expireLoanOffer expira uma oferta de empréstimo após timeout
func expireLoanOffer(s *discordgo.Session, loanID, channelID, messageID string) {
	pendingMu.Lock()
	var borrowerID string
	for uid, req := range pendingLoans {
		if req.Loan.ID == loanID {
			borrowerID = uid
			delete(pendingLoans, uid)
			break
		}
	}
	pendingMu.Unlock()

	if borrowerID != "" {
		content := fmt.Sprintf("⏰ **Loan offer expired!** <@%s> did not respond in time.", borrowerID)
		embeds := []*discordgo.MessageEmbed{}
		components := []discordgo.MessageComponent{}
		s.ChannelMessageEditComplex(&discordgo.MessageEdit{
			Channel:    channelID,
			ID:         messageID,
			Content:    &content,
			Embeds:     &embeds,
			Components: &components,
		})
	}
}

// processLoanPayment processa o pagamento de um empréstimo
func processLoanPayment(s *discordgo.Session, channelID string, loan *database.Loan, payerID string) {
	// Transferir do devedor para o credor
	err := database.TransferCoins(loan.BorrowerID, loan.LenderID, loan.TotalOwed)
	if err != nil {
		s.ChannelMessageSendEmbed(channelID, utils.ErrorEmbed(
			fmt.Sprintf("Error processing payment. You need %d %s.", loan.TotalOwed, config.Bot.CurrencySymbol)))
		return
	}

	// Marcar como pago
	loansMu.Lock()
	loan.Paid = true
	delete(loans, loan.ID)
	loansMu.Unlock()

	// Atualizar no banco
	database.MarkLoanAsPaid(loan.ID)

	// Enviar confirmação
	s.ChannelMessageSendEmbed(channelID, utils.SuccessEmbed("Loan Paid!",
		fmt.Sprintf("<@%s> paid **%d %s** to <@%s>**!**\nLoan `%s` is now fully repaid! 🎉",
			payerID, loan.TotalOwed, config.Bot.CurrencySymbol, loan.LenderID, loan.ID)))
}

// scheduleAutoCollection agenda a cobrança automática no vencimento
func scheduleAutoCollection(s *discordgo.Session, loan *database.Loan) {
	timeUntilDue := loan.DueDate.Sub(time.Now())
	if timeUntilDue <= 0 {
		// Já venceu, cobrar imediatamente
		go autoCollectLoan(s, loan)
		return
	}

	// Agendar cobrança
	time.AfterFunc(timeUntilDue, func() {
		autoCollectLoan(s, loan)
	})
}

// autoCollectLoan cobra automaticamente o empréstimo no vencimento
func autoCollectLoan(s *discordgo.Session, loan *database.Loan) {
	loansMu.RLock()
	// Verificar se ainda existe e não foi pago
	currentLoan, exists := loans[loan.ID]
	loansMu.RUnlock()

	if !exists || currentLoan.Paid {
		return
	}

	// Tentar cobrar
	borrowerBalance := database.GetBalance(loan.BorrowerID)

	if borrowerBalance >= loan.TotalOwed {
		// Tem saldo suficiente, cobrar
		database.TransferCoins(loan.BorrowerID, loan.LenderID, loan.TotalOwed)

		loansMu.Lock()
		loan.Paid = true
		delete(loans, loan.ID)
		loansMu.Unlock()

		database.MarkLoanAsPaid(loan.ID)

		// Notificar
		s.ChannelMessageSendEmbed(loan.ChannelID, utils.SuccessEmbed("Auto Payment Executed",
			fmt.Sprintf("💰 Loan auto-collected!\n<@%s> paid **%d %s** to <@%s>.\nLoan `%s` is now fully repaid! ✅",
				loan.BorrowerID, loan.TotalOwed, config.Bot.CurrencySymbol, loan.LenderID, loan.ID)))
	} else {
		// Não tem saldo suficiente, deixar negativo
		// Primeiro zera o saldo atual (vai para o credor)
		if borrowerBalance > 0 {
			database.TransferCoins(loan.BorrowerID, loan.LenderID, borrowerBalance)
		}

		// Adiciona o restante como dívida (saldo negativo)
		remaining := loan.TotalOwed - borrowerBalance
		database.AddCoins(loan.BorrowerID, -remaining)

		loansMu.Lock()
		loan.Paid = true
		delete(loans, loan.ID)
		loansMu.Unlock()

		database.MarkLoanAsPaid(loan.ID)

		// Notificar
		s.ChannelMessageSendEmbed(loan.ChannelID, &discordgo.MessageEmbed{
			Title:       "⚠️ Loan Defaulted",
			Description: fmt.Sprintf("**LOAN DEFAULTED**\n<@%s> didn't have enough funds!\n"+
				"Collected: **%d %s** | Remaining debt: **%d %s**\n"+
				"Loan `%s` marked as paid with negative balance! 💸",
				loan.BorrowerID, borrowerBalance, config.Bot.CurrencySymbol, remaining, config.Bot.CurrencySymbol, loan.ID),
			Color: 0xFF0000,
		})
	}
}

// formatDuration formata a duração para exibição
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

// LoadActiveLoans carrega empréstimos ativos do banco ao iniciar
func LoadActiveLoans(s *discordgo.Session) {
	activeLoans, err := database.GetActiveLoans()
	if err != nil {
		return
	}

	loansMu.Lock()
	for _, loan := range activeLoans {
		loans[loan.ID] = loan
		// Reagendar cobrança
		go scheduleAutoCollection(s, loan)
	}
	loansMu.Unlock()
}
