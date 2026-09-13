package commands

import (
	"github.com/bwmarrin/discordgo"
)

func init() {
	min := 1.0
	dm := false

	page := func() *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        "pagina",
			Description: "Número da página",
			MinValue:    &min,
			MaxValue:    100000,
		}
	}
	member := func(name, desc string, required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionUser,
			Name:        name,
			Description: desc,
			Required:    required,
		}
	}
	character := func(name, description string, required bool) *discordgo.ApplicationCommandOption {
		return &discordgo.ApplicationCommandOption{
			Type:        discordgo.ApplicationCommandOptionInteger,
			Name:        name,
			Description: description,
			Required:    required,
			MinValue:    &min,
		}
	}

	poolChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: "🌸 Waifus (Anime)", Value: "wa"},
		{Name: "⚔️ Husbandos (Anime)", Value: "ha"},
		{Name: "🌟 Todos (Anime)", Value: "ma"},
		{Name: "🎮 Waifus (Games)", Value: "wg"},
		{Name: "🕹️ Husbandos (Games)", Value: "hg"},
		{Name: "👾 Todos (Games)", Value: "mg"},
		{Name: "💖 Todas Waifus (Geral)", Value: "w"},
		{Name: "🖤 Todos Husbandos (Geral)", Value: "h"},
		{Name: "🎲 Roleta Geral (Todos)", Value: "roll"},
	}

	claimChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: "✨ Todos os personagens", Value: "all"},
		{Name: "🔓 Não casados (Livres / Unclaimed)", Value: "unclaimed"},
		{Name: "💍 Já casados (Claimed)", Value: "claimed"},
	}

	genderChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: "👑 Todos os gêneros", Value: "all"},
		{Name: "🌸 Mulheres (Waifus)", Value: "female"},
		{Name: "⚔️ Homens (Husbandos)", Value: "male"},
	}

	haremModeChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: "📋 Lista Compacta", Value: "list"},
		{Name: "📷 Visual com Fotos", Value: "visual"},
	}

	wishlistActionChoices := []*discordgo.ApplicationCommandOptionChoice{
		{Name: "📜 Ver lista de desejos", Value: "wishes"},
		{Name: "➕ Adicionar personagem aos desejos", Value: "wish"},
		{Name: "➖ Remover personagem dos desejos", Value: "unwish"},
	}

	SlashCommands = append(SlashCommands,
		// 1. /roll
		&discordgo.ApplicationCommand{
			Name:         "roll",
			Description:  "Sortear um personagem de anime ou jogos para colecionar",
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{
					Type:        discordgo.ApplicationCommandOptionString,
					Name:        "pool",
					Description: "Categoria de personagens a sortear (Waifus, Husbandos, Anime, Games)",
					Choices:     poolChoices,
				},
			},
		},

		// 2. /top
		&discordgo.ApplicationCommand{
			Name:         "top",
			Description:  "Ranking dos personagens mais populares do catálogo",
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "genero", Description: "Filtrar por gênero (Waifus / Husbandos)", Choices: genderChoices},
				{Type: discordgo.ApplicationCommandOptionString, Name: "posse", Description: "Filtrar por posse no servidor (Livres / Casados)", Choices: claimChoices},
				page(),
			},
		},

		// 3. /info
		&discordgo.ApplicationCommand{
			Name:         "info",
			Description:  "Ver foto oficial, obra, valor e dono de um personagem",
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "personagem", Description: "Nome ou ID do personagem", Required: true},
			},
		},

		// 4. /harem
		&discordgo.ApplicationCommand{
			Name:         "harem",
			Description:  "Visualizar sua coleção de personagens ou a de outro membro",
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				member("membro", "Membro do servidor (deixe vazio para ver o seu)", false),
				{Type: discordgo.ApplicationCommandOptionString, Name: "modo", Description: "Formato de exibição (Lista ou Fotos)", Choices: haremModeChoices},
				page(),
			},
		},

		// 5. /perfil
		&discordgo.ApplicationCommand{
			Name:         "perfil",
			Description:  "Ver seus rolls disponíveis, tempo de claim e status no gacha",
			DMPermission: &dm,
		},

		// 6. /galeria
		&discordgo.ApplicationCommand{
			Name:         "galeria",
			Description:  "Navegar pelas fotos e ilustrações aprovadas de um personagem",
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				character("personagem", "ID do personagem", true),
				page(),
			},
		},

		// 7. /wishlist
		&discordgo.ApplicationCommand{
			Name:         "wishlist",
			Description:  "Gerenciar sua lista de personagens desejados",
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				{Type: discordgo.ApplicationCommandOptionString, Name: "acao", Description: "O que deseja fazer?", Required: true, Choices: wishlistActionChoices},
				character("personagem", "ID do personagem (necessário para adicionar ou remover)", false),
			},
		},

		// 8. /troca
		&discordgo.ApplicationCommand{
			Name:         "troca",
			Description:  "Propor uma troca de personagens com outro jogador",
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				member("membro", "Membro com quem deseja trocar", true),
				character("seu_personagem", "ID do seu personagem a oferecer", true),
				character("personagem_desejado", "ID do personagem que você quer em troca", true),
			},
		},

		// 9. /presente
		&discordgo.ApplicationCommand{
			Name:         "presente",
			Description:  "Enviar um personagem do seu harém como presente para outro jogador",
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				member("membro", "Membro que receberá o presente", true),
				character("personagem", "ID do seu personagem a presentear", true),
			},
		},

		// 10. /divorcio
		&discordgo.ApplicationCommand{
			Name:         "divorcio",
			Description:  "Libertar um personagem do seu harém em troca de moedas do servidor",
			DMPermission: &dm,
			Options: []*discordgo.ApplicationCommandOption{
				character("personagem", "ID do personagem a divorciar", true),
			},
		},
	)
}
