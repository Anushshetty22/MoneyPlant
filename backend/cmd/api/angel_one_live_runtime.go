package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/config"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

// startOptionalAngelOneMonitor resolves the current provider catalog before
// starting one multiplexed monitor for all configured canonical symbols.
func startOptionalAngelOneMonitor(
	ctx context.Context,
	cfg config.Config,
	store *ingestion.LiveMarketSnapshotStore,
	statusRegistry *ingestion.LiveMonitorStatusRegistry,
	snapshotRepository *database.LiveMarketSnapshotRepository,
	sourceRepository *database.InstrumentSourceRepository,
) {
	if len(cfg.AngelOneLiveMonitorSymbols) == 0 {
		return
	}

	resolveContext, cancelResolve := context.WithTimeout(ctx, 5*time.Second)
	subscriptions, err := resolveAngelOneLiveSubscriptions(resolveContext, sourceRepository, cfg.AngelOneLiveMonitorSymbols)
	cancelResolve()
	if err != nil {
		for _, symbol := range cfg.AngelOneLiveMonitorSymbols {
			statusRegistry.Register(ingestion.InstrumentReference{
				CanonicalSymbol: symbol,
				Provider:        ingestion.ProviderAngelOne,
				ProviderSymbol:  symbol,
			}).MarkError(err)
		}
		log.Printf("Angel One live catalog resolution error: %v", err)
		return
	}

	statusStores := make(map[string]*ingestion.LiveMonitorStatusStore, len(subscriptions))
	for _, subscription := range subscriptions {
		statusStores[normalizeRuntimeSymbol(subscription.ProviderSymbol)] = statusRegistry.Register(ingestion.InstrumentReference{
			CanonicalSymbol:      subscription.CanonicalSymbol,
			Provider:             ingestion.ProviderAngelOne,
			ProviderSymbol:       subscription.ProviderSymbol,
			ProviderInstrumentID: subscription.ProviderInstrumentID,
		})
	}

	go func() {
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
			markAngelOneLiveSetupError(statusStores, err)
			return
		}
		provider, err := ingestion.NewAngelOneLiveMarketDataProvider(nil, authenticator, ingestion.AngelOneSession{}, ingestion.AngelOneSmartStreamURL)
		if err != nil {
			markAngelOneLiveSetupError(statusStores, err)
			return
		}
		multiplexedProvider, err := provider.MultiplexedProvider(subscriptions)
		if err != nil {
			markAngelOneLiveSetupError(statusStores, err)
			return
		}

		policy := ingestion.DefaultLiveReconnectPolicy()
		policy.MaxRetries = cfg.LiveMonitorMaxRetries
		policy.OnRetry = func(reconnectErr error, _ time.Duration) {
			for _, statusStore := range statusStores {
				statusStore.RecordReconnect(reconnectErr)
			}
		}
		stream, err := ingestion.NewReconnectingLiveMarketStream(
			multiplexedProvider,
			ingestion.LiveMarketStreamRequest{ProviderSymbol: "angel-one-multiplexed"},
			policy,
		)
		if err != nil {
			markAngelOneLiveSetupError(statusStores, err)
			return
		}
		monitor, err := ingestion.NewLiveMarketMonitor(stream)
		if err != nil {
			markAngelOneLiveSetupError(statusStores, err)
			return
		}

		statusSink := newMultiplexedLiveMonitorStatusSink(statusStores)
		persist := liveSnapshotPersistence(func(persistContext context.Context, event ingestion.LiveMarketEvent) error {
			return persistLiveSnapshot(persistContext, snapshotRepository, event)
		})
		result, _ := runLiveMonitor(
			ctx,
			monitor,
			store,
			statusSink,
			persist,
			cfg.LiveMonitorPersistenceInterval,
			cfg.LiveMonitorFinalPersistenceTimeout,
		)
		log.Printf("Angel One live monitor summary: received=%d accepted=%d rejected=%d snapshots=%d", result.Received, result.Accepted, result.Rejected, store.Count())
	}()
}

func resolveAngelOneLiveSubscriptions(
	ctx context.Context,
	repository *database.InstrumentSourceRepository,
	symbols []string,
) ([]ingestion.AngelOneLiveSubscription, error) {
	if repository == nil {
		return nil, fmt.Errorf("Angel One instrument source repository is not configured")
	}
	subscriptions := make([]ingestion.AngelOneLiveSubscription, 0, len(symbols))
	for _, symbol := range symbols {
		sources, err := repository.ListByCanonicalSymbol(ctx, symbol)
		if err != nil {
			return nil, fmt.Errorf("resolve Angel One source for %s: %w", symbol, err)
		}
		var selected *database.InstrumentSource
		for index := range sources {
			source := &sources[index]
			if source.Provider != string(ingestion.ProviderAngelOne) || !source.IsActive || !source.IsAuthoritative || source.ProviderInstrumentID == nil || strings.TrimSpace(*source.ProviderInstrumentID) == "" || strings.TrimSpace(source.ProviderSymbol) == "" {
				continue
			}
			if selected != nil {
				return nil, fmt.Errorf("multiple active Angel One sources exist for %s", symbol)
			}
			selected = source
		}
		if selected == nil {
			return nil, fmt.Errorf("no active Angel One source mapping exists for %s", symbol)
		}
		exchangeType, err := angelOneExchangeType(selected.Metadata)
		if err != nil {
			return nil, fmt.Errorf("resolve Angel One exchange for %s: %w", symbol, err)
		}
		subscriptions = append(subscriptions, ingestion.AngelOneLiveSubscription{
			CanonicalSymbol:      strings.ToUpper(strings.TrimSpace(symbol)),
			ProviderSymbol:       selected.ProviderSymbol,
			ProviderInstrumentID: *selected.ProviderInstrumentID,
			ExchangeType:         exchangeType,
		})
	}
	return subscriptions, nil
}

func angelOneExchangeType(metadata []byte) (byte, error) {
	var values struct {
		ExchangeSegment string `json:"exchange_segment"`
	}
	if err := json.Unmarshal(metadata, &values); err != nil {
		return 0, fmt.Errorf("decode source metadata: %w", err)
	}
	switch strings.ToUpper(strings.TrimSpace(values.ExchangeSegment)) {
	case "NSE", "NSE_CM", "NSE_CASH":
		return 1, nil
	case "BSE", "BSE_CM", "BSE_CASH":
		return 3, nil
	default:
		return 0, fmt.Errorf("unsupported exchange segment %q", values.ExchangeSegment)
	}
}

func markAngelOneLiveSetupError(stores map[string]*ingestion.LiveMonitorStatusStore, err error) {
	for _, store := range stores {
		store.MarkError(err)
	}
	log.Printf("Angel One live monitor setup error: %v", err)
}

func normalizeRuntimeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}
