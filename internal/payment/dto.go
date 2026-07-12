package payment

type WebhookRequest struct {
	PaymentID             string `json:"payment_id"`
	RentalID              string `json:"rental_id"`
	ExternalTransactionID string `json:"external_transaction_id"`
	Provider              string `json:"provider"`
	Amount                int64  `json:"amount"`
	Currency              string `json:"currency"`
	Status                string `json:"status"`
}

type WebhookResponse struct {
	Status  string `json:"status"`
	Message string `json:"message,omitempty"`
}
