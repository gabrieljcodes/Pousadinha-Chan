package commands

import "github.com/bwmarrin/discordgo"

func init() {
	choices := []*discordgo.ApplicationCommandOptionChoice{}
	for _, v := range []struct{ name, value string }{{"Sortear", "roll"}, {"Minha coleção", "collection"}, {"Buscar personagem", "search"}, {"Ver personagem", "character"}, {"Galeria", "gallery"}, {"Desejar", "wish"}, {"Remover desejo", "unwish"}, {"Meus desejos", "wishes"}, {"Meus limites", "status"}, {"Ajuda", "help"}} {
		choices = append(choices, &discordgo.ApplicationCommandOptionChoice{Name: v.name, Value: v.value})
	}
	min := 1.0
	SlashCommands = append(SlashCommands, &discordgo.ApplicationCommand{Name: "gacha", Description: "Colecione personagens e descubra novas histórias", Options: []*discordgo.ApplicationCommandOption{
		{Type: discordgo.ApplicationCommandOptionString, Name: "acao", Description: "O que você quer fazer?", Required: true, Choices: choices},
		{Type: discordgo.ApplicationCommandOptionString, Name: "busca", Description: "Nome para buscar ou ID do personagem", MaxLength: 100},
		{Type: discordgo.ApplicationCommandOptionInteger, Name: "pagina", Description: "Página da coleção ou imagem da galeria", MinValue: &min, MaxValue: 100000},
	}})
}
