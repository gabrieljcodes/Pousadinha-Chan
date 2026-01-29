package crypto

// Crypto represents a cryptocurrency
type Crypto struct {
	ID     string // CoinGecko ID (e.g., "bitcoin", "ethereum")
	Symbol string // Trading symbol (e.g., "BTC", "ETH")
	Name   string // Full name (e.g., "Bitcoin", "Ethereum")
	Type   string // "major" or "meme"
}

// CryptoResponse represents the API response from CoinGecko
type CryptoResponse map[string]map[string]float64

// AvailableCryptos lista todas as criptomoedas disponíveis
var AvailableCryptos = []Crypto{
	// Major Cryptocurrencies
	{ID: "bitcoin", Symbol: "BTC", Name: "Bitcoin", Type: "major"},
	{ID: "ethereum", Symbol: "ETH", Name: "Ethereum", Type: "major"},
	{ID: "binancecoin", Symbol: "BNB", Name: "BNB", Type: "major"},
	{ID: "solana", Symbol: "SOL", Name: "Solana", Type: "major"},
	{ID: "ripple", Symbol: "XRP", Name: "XRP", Type: "major"},
	{ID: "cardano", Symbol: "ADA", Name: "Cardano", Type: "major"},
	{ID: "dogecoin", Symbol: "DOGE", Name: "Dogecoin", Type: "meme"},
	{ID: "polkadot", Symbol: "DOT", Name: "Polkadot", Type: "major"},
	{ID: "polygon", Symbol: "MATIC", Name: "Polygon", Type: "major"},
	{ID: "avalanche-2", Symbol: "AVAX", Name: "Avalanche", Type: "major"},
	{ID: "chainlink", Symbol: "LINK", Name: "Chainlink", Type: "major"},
	{ID: "litecoin", Symbol: "LTC", Name: "Litecoin", Type: "major"},
	
	// Meme Coins (High volatility)
	{ID: "shiba-inu", Symbol: "SHIB", Name: "Shiba Inu", Type: "meme"},
	{ID: "pepe", Symbol: "PEPE", Name: "Pepe", Type: "meme"},
	{ID: "bonk", Symbol: "BONK", Name: "Bonk", Type: "meme"},
	{ID: "floki", Symbol: "FLOKI", Name: "FLOKI", Type: "meme"},
	{ID: "dogwifcoin", Symbol: "WIF", Name: "dogwifhat", Type: "meme"},
	{ID: "memecoin", Symbol: "MEME", Name: "Memecoin", Type: "meme"},
	{ID: "coq-inu", Symbol: "COQ", Name: "Coq Inu", Type: "meme"},
	{ID: "snek", Symbol: "SNEK", Name: "Snek", Type: "meme"},
	{ID: "mog-coin", Symbol: "MOG", Name: "Mog Coin", Type: "meme"},
	{ID: "grok", Symbol: "GROK", Name: "Grok", Type: "meme"},
}

// GetCryptoBySymbol retorna uma crypto pelo símbolo
func GetCryptoBySymbol(symbol string) *Crypto {
	for _, c := range AvailableCryptos {
		if c.Symbol == symbol {
			return &c
		}
	}
	return nil
}

// GetCryptoByID retorna uma crypto pelo ID do CoinGecko
func GetCryptoByID(id string) *Crypto {
	for _, c := range AvailableCryptos {
		if c.ID == id {
			return &c
		}
	}
	return nil
}
