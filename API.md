# Pousadinha-Chan API Documentation

The Pousadinha-Chan API allows you to interact with your account and the economy system programmatically.

## Base URL
By default, the API runs on port 8080 (configurable in `config.json`).
`http://localhost:8080/api/v1`

## Authentication
All requests must include your API Key in the `X-API-Key` header.

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

### 1. Get My Profile
Returns your current balance.

*   **URL:** `/me`
*   **Method:** `GET`
*   **Response Success (200 OK):**
    ```json
    {
      "user_id": "123456789012345678",
      "balance": 1500
    }
    ```
*   **Response Error (401 Unauthorized):**
    ```json
    {
      "error": "Invalid API Key"
    }
    ```

### 2. Transfer Coins
Send coins to another user.

*   **URL:** `/transfer`
*   **Method:** `POST`
*   **Headers:** `Content-Type: application/json`
*   **Body:**
    ```json
    {
      "to_user_id": "987654321098765432",
      "amount": 500
    }
    ```
*   **Response Success (200 OK):**
    ```json
    {
      "status": "success"
    }
    ```
*   **Response Error (400 Bad Request):**
    *   Insufficient funds
    *   Invalid amount
    *   Self-transfer attempt

## Managing Keys
Use the Discord Slash Commands:
*   `/apikey create [name]` - Generate a new key.
*   `/apikey list` - See your active keys (masked).
*   `/apikey delete <prefix>` - Revoke a key using its first 5 characters.
