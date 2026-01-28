package stockmarket

type StockResponse struct {
	Ticker           string  `json:"Ticker"`
	Name             string  `json:"Name"`
	Price            float64 `json:"Price"`
	ChangeAmount     float64 `json:"ChangeAmount"`
	ChangePercentage float64 `json:"ChangePercentage"`
}

type Company struct {
	Ticker string `json:"Ticker"`
	Name   string `json:"Name"`
}
