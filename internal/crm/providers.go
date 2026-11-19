package crm

import (
	"context"
	"errors"
)

// ListProviders returns one row per registered signup.Provider (from
// signup.All), joined with provider_connections. Providers are never listed
// anywhere else.
func (s *Service) ListProviders(ctx context.Context) ([]ProviderStatus, error) {
	return nil, errors.New("not implemented")
}

// ConnectProvider stores the webhook secret for a registered provider,
// replacing any previous one. When secret is "" and the provider has
// IssueSecret, a random "whsec_<base64 32 bytes>" is generated and returned
// so the UI can show it once for the user to paste into the provider.
func (s *Service) ConnectProvider(ctx context.Context, actor Actor, name, secret string) (string, error) {
	return "", errors.New("not implemented")
}

// DisconnectProvider deletes the connection. Deliveries then get ErrNotConnected.
// Identities and contacts from that provider stay.
func (s *Service) DisconnectProvider(ctx context.Context, actor Actor, name string) error {
	return errors.New("not implemented")
}
