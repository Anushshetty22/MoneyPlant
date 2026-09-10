// Command auth-angelone verifies the local Angel One credentials and creates a
// short-lived in-memory session. It never prints or persists token values.
package main

import (
	// context provides cancellation for the bounded authentication request.
	"context"
	// log reports only safe session metadata.
	"log"
	// time bounds startup and network failure handling.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("configuration error: %v", err)
	}

	authenticator, err := ingestion.NewAngelOneAuthenticatorWithSettings(
		nil,
		ingestion.AngelOneAuthSettings{
			BaseURL:        cfg.AngelOneBaseURL,
			ClientLocalIP:  cfg.AngelOneClientLocalIP,
			ClientPublicIP: cfg.AngelOneClientPublicIP,
			MACAddress:     cfg.AngelOneMACAddress,
		},
		ingestion.AngelOneCredentials{
			APIKey:     cfg.AngelOneAPIKey,
			ClientCode: cfg.AngelOneClientCode,
			Password:   cfg.AngelOnePassword,
			TOTPSecret: cfg.AngelOneTOTPSecret,
		},
	)
	if err != nil {
		log.Fatalf("Angel One credential configuration error: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	session, err := authenticator.Login(ctx)
	if err != nil {
		log.Fatalf("Angel One authentication failed: %v", err)
	}

	log.Printf("Angel One authentication succeeded at %s; JWT, refresh, and feed tokens received in memory", session.ObtainedAt().UTC().Format(time.RFC3339))
}
