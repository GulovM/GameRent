package payment

type WebhookRequest struct {
	PaymentID             string `json:"payment_id"`
	RentalID              string `json:"rental_id"`
	ExternalTransactionID string `json:"external_transaction_id"`
	Status                string `json:"status"`
}

type CredentialsPayload struct {
	Login     string `json:"login"`
	Password  string `json:"password"`
	SteamID64 string `json:"steam_id64"`
}

type WebhookResponse struct {
	Status      string              `json:"status"`
	Message     string              `json:"message,omitempty"`
	Credentials *CredentialsPayload `json:"credentials,omitempty"`
}
