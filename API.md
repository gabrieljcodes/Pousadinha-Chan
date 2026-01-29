# Pousadinha-Chan API Documentation

The Pousadinha-Chan API allows you to interact with your account and the economy system programmatically.

## Base URL

By default, the API runs on port 8080 (configurable in `config.json`).

```text
http://localhost:8080/api/v1
```

## Authentication

All requests (except listing stocks/crypto) must include your API Key in the `X-API-Key` header.

**To get an API Key:**

1. In Discord, use `/apikey create`.
2. The bot will send the key to your DM.
3. Save it immediately! The message is deleted after 60 seconds.

**Example Header:**

```http
X-API-Key: 550e8400-e29b-41d4-a716-446655440000
```

---

## Endpoints

### User Endpoints

#### 1. Get My Profile

Returns your current balance.

* **URL:** `/me`
* **Method:** `GET`
* **Headers:** `X-API-Key: <your-api-key>`
* **Response Success (200 OK):**

    ```json
    {
      "user_id": "123456789012345678",
      "balance": 1500
    }
    ```

* **Response Error (401 Unauthorized):**

    ```json
    {
      "error": "Invalid API Key"
    }
    ```

#### 2. Transfer Coins

Send coins to another user.

* **URL:** `/transfer`
* **Method:** `POST`
* **Headers:**
  * `X-API-Key: <your-api-key>`
  * `Content-Type: application/json`
* **Body:**

    ```json
    {
      "to_user_id": "987654321098765432",
      "amount": 500
    }
    ```

* **Response Success (200 OK):**

    ```json
    {
      "status": "success"
    }
    ```

* **Response Error (400 Bad Request):**
  * Insufficient funds
  * Invalid amount
  * Self-transfer attempt

---

### Stock Market Endpoints

#### 3. List Available Stocks

Returns all available stocks with current prices and market changes. **No authentication required.**

* **URL:** `/stocks`
* **Method:** `GET`
* **Response Success (200 OK):**

    ```json
    [
      {
        "ticker": "AAPL",
        "name": "Apple Inc.",
        "price": 195.89,
        "change_amount": 2.45,
        "change_percentage": 1.27
      },
      {
        "ticker": "GOOGL",
        "name": "Alphabet Inc.",
        "price": 141.80,
        "change_amount": -0.92,
        "change_percentage": -0.64
      }
    ]
    ```

* **Notes:**
  * Prices are updated every 10 minutes
  * `change_amount` and `change_percentage` show daily market changes

#### 4. Get My Stock Portfolio

Returns your current stock investments.

* **URL:** `/stocks/portfolio`
* **Method:** `GET`
* **Headers:** `X-API-Key: <your-api-key>`
* **Response Success (200 OK):**

    ```json
    {
      "items": [
        {
          "ticker": "AAPL",
          "name": "Apple Inc.",
          "shares": 10.2564,
          "current_price": 195.89,
          "value": 2009
        },
        {
          "ticker": "TSLA",
          "name": "Tesla Inc.",
          "shares": 5.0000,
          "current_price": 248.50,
          "value": 1242
        }
      ],
      "total_value": 3251
    }
    ```

* **Response Success with no investments (200 OK):**

    ```json
    {
      "items": [],
      "total_value": 0
    }
    ```

#### 5. Buy Stocks

Purchase shares of a company using your coin balance.

* **URL:** `/stocks/buy`
* **Method:** `POST`
* **Headers:**
  * `X-API-Key: <your-api-key>`
  * `Content-Type: application/json`
* **Body:**

    ```json
    {
      "ticker": "AAPL",
      "amount": 2000
    }
    ```

  * `ticker`: Company ticker symbol (e.g., "AAPL", "GOOGL")
  * `amount`: Amount of coins to spend
* **Response Success (200 OK):**

    ```json
    {
      "ticker": "AAPL",
      "shares": 10.2108,
      "amount_paid": 2000,
      "price_per_share": 195.87,
      "balance": 500
    }
    ```

* **Response Error (400 Bad Request):**

    ```json
    {
      "error": "Invalid ticker"
    }
    ```

    ```json
    {
      "error": "Insufficient funds"
    }
    ```

* **Notes:**
  * Shares are calculated as `amount / current_price`
  * The transaction is atomic - either both the coin deduction and share addition succeed, or both fail

#### 6. Sell Stocks

Sell shares of a company for coins.

* **URL:** `/stocks/sell`
* **Method:** `POST`
* **Headers:**
  * `X-API-Key: <your-api-key>`
  * `Content-Type: application/json`
* **Body:**

    ```json
    {
      "ticker": "AAPL",
      "shares": 5.0
    }
    ```

  * `ticker`: Company ticker symbol
  * `shares`: Number of shares to sell (can be fractional)
* **Response Success (200 OK):**

    ```json
    {
      "ticker": "AAPL",
      "shares": 5.0,
      "amount_received": 979,
      "price_per_share": 195.89,
      "balance": 1479
    }
    ```

* **Response Error (400 Bad Request):**

    ```json
    {
      "error": "You don't own any shares of this company"
    }
    ```

    ```json
    {
      "error": "You only own 10.2108 shares"
    }
    ```

* **Notes:**
  * You can sell fractional shares
  * If you sell all remaining shares, the investment record is removed

---

### Cryptocurrency Endpoints

#### 7. List Available Cryptocurrencies

Returns all available cryptocurrencies with current prices. **No authentication required.**

* **URL:** `/crypto`
* **Method:** `GET`
* **Response Success (200 OK):**

    ```json
    [
      {
        "symbol": "BTC",
        "name": "Bitcoin",
        "type": "major",
        "price": 43520.50
      },
      {
        "symbol": "ETH",
        "name": "Ethereum",
        "type": "major",
        "price": 2280.75
      },
      {
        "symbol": "DOGE",
        "name": "Dogecoin",
        "type": "meme",
        "price": 0.089
      },
      {
        "symbol": "PEPE",
        "name": "Pepe",
        "type": "meme",
        "price": 0.00000123
      }
    ]
    ```

* **Notes:**
  * Prices are fetched in real-time from CoinGecko
  * `type` can be "major" (established coins) or "meme" (high volatility)
  * Meme coins are highly volatile - invest at your own risk!

#### 8. Get My Crypto Portfolio

Returns your current cryptocurrency investments.

* **URL:** `/crypto/portfolio`
* **Method:** `GET`
* **Headers:** `X-API-Key: <your-api-key>`
* **Response Success (200 OK):**

    ```json
    {
      "items": [
        {
          "symbol": "BTC",
          "name": "Bitcoin",
          "type": "major",
          "coins": 0.0523,
          "current_price": 43520.50,
          "value": 2276
        },
        {
          "symbol": "DOGE",
          "name": "Dogecoin",
          "type": "meme",
          "coins": 15000.5,
          "current_price": 0.089,
          "value": 1335
        }
      ],
      "total_value": 3611
    }
    ```

#### 9. Buy Cryptocurrency

Purchase cryptocurrency using your coin balance.

* **URL:** `/crypto/buy`
* **Method:** `POST`
* **Headers:**
  * `X-API-Key: <your-api-key>`
  * `Content-Type: application/json`
* **Body:**

    ```json
    {
      "symbol": "BTC",
      "amount": 1000
    }
    ```

  * `symbol`: Cryptocurrency symbol (e.g., "BTC", "ETH", "DOGE")
  * `amount`: Amount of coins to spend
* **Response Success (200 OK):**

    ```json
    {
      "symbol": "BTC",
      "coins": 0.02297,
      "amount_paid": 1000,
      "price": 43520.50,
      "balance": 4000
    }
    ```

* **Response Error (400 Bad Request):**

    ```json
    {
      "error": "Invalid cryptocurrency symbol"
    }
    ```

    ```json
    {
      "error": "Insufficient funds"
    }
    ```

#### 10. Sell Cryptocurrency

Sell cryptocurrency for coins.

* **URL:** `/crypto/sell`
* **Method:** `POST`
* **Headers:**
  * `X-API-Key: <your-api-key>`
  * `Content-Type: application/json`
* **Body:**

    ```json
    {
      "symbol": "BTC",
      "coins": 0.01
    }
    ```

  * `symbol`: Cryptocurrency symbol
  * `coins`: Amount of coins to sell (can be fractional)
* **Response Success (200 OK):**

    ```json
    {
      "symbol": "BTC",
      "coins": 0.01,
      "amount_received": 435,
      "price": 43520.50,
      "balance": 4435
    }
    ```

* **Response Error (400 Bad Request):**

    ```json
    {
      "error": "You don't own any of this cryptocurrency"
    }
    ```

    ```json
    {
      "error": "You only own 0.02297 BTC"
    }
    ```

---

## Managing API Keys

Use the Discord Slash Commands:

* `/apikey create [name]` - Generate a new key.
* `/apikey list` - See your active keys (masked).
* `/apikey delete <prefix>` - Revoke a key using its first 5 characters.

---

## Webhooks

You can configure a webhook URL using `/webhook set <url>` in Discord to receive notifications for:

* Transfer received
* Stock purchases
* Stock sales
* Crypto purchases
* Crypto sales

The webhook will receive a simple message payload:

```json
{
  "content": "📈 **Stock Purchase**\nYou bought **10.2108** shares of **AAPL**..."
}
```

---

## Error Responses

All error responses follow this format:

```json
{
  "error": "Error description here"
}
```

Common HTTP status codes:

| Code | Meaning |
| ------ | --------- |
| 200 | Success |
| 400 | Bad Request - Invalid parameters or insufficient funds/shares |
| 401 | Unauthorized - Invalid or missing API key |
| 405 | Method Not Allowed - Wrong HTTP method |
| 500 | Internal Server Error - Database or server error |
| 503 | Service Unavailable - Could not fetch prices |

## Valorant Betting API (em desenvolvimento)

**Autenticação:** Mesma API Key do usuário (header `X-User-ID` ou via chave)

### POST /api/v1/valorant/bet

Cria uma aposta em perda/vitória de jogador

Body:

```json
{
  "riot_id": "PlayerName#TAG",
  "amount": 500,
  "bet_on_loss": true
}

Response (200):

{
  "success": true,
  "bet_id": 42,
  "message": "Bet placed – awaiting match result"
}

GET /api/v1/valorant/pending

Lista apostas pendentes do usuário autenticado

Response:

{
  "bets": [
    {
      "id": 42,
      "riot_id": "PlayerName#TAG",
      "amount": 500,
      "bet_on_loss": true,
      "created_at": "2026-01-29T14:00:00Z",
      "resolved": false
    }
  ]
}
