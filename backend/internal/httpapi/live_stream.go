package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/ingestion"
)

// LiveStreamHub fans provider-neutral live updates out to independent browser
// subscribers. A slow or disconnected browser never blocks the provider
// monitor because subscriber channels are buffered and writes are non-blocking.
type LiveStreamHub struct {
	mu          sync.RWMutex
	nextID      uint64
	subscribers map[uint64]*liveStreamSubscriber
}

type liveStreamSubscriber struct {
	provider        string
	canonicalSymbol string
	updates         chan liveStreamUpdate
}

type liveStreamUpdate struct {
	event string
	data  any
}

// NewLiveStreamHub creates the process-local fan-out used by the API server.
func NewLiveStreamHub() *LiveStreamHub {
	return &LiveStreamHub{subscribers: make(map[uint64]*liveStreamSubscriber)}
}

// PublishSnapshot sends one latest-trade update to matching subscribers.
func (h *LiveStreamHub) PublishSnapshot(event ingestion.LiveMarketEvent) {
	if h == nil {
		return
	}
	h.publish(liveStreamUpdate{event: "snapshot", data: liveSnapshotResponseFromEvent(event)}, string(event.Provider), event.CanonicalSymbol)
}

// PublishStatus sends one lifecycle update to matching subscribers.
func (h *LiveStreamHub) PublishStatus(status ingestion.LiveMonitorStatus) {
	if h == nil {
		return
	}
	h.publish(liveStreamUpdate{event: "status", data: liveMonitorStatusResponseFrom(status)}, status.Provider, status.CanonicalSymbol)
}

func (h *LiveStreamHub) publish(update liveStreamUpdate, provider, canonicalSymbol string) {
	normalizedProvider := strings.ToLower(strings.TrimSpace(provider))
	normalizedSymbol := strings.ToUpper(strings.TrimSpace(canonicalSymbol))
	h.mu.RLock()
	defer h.mu.RUnlock()
	for _, subscriber := range h.subscribers {
		if subscriber.provider != "" && subscriber.provider != normalizedProvider {
			continue
		}
		if subscriber.canonicalSymbol != "" && subscriber.canonicalSymbol != normalizedSymbol {
			continue
		}
		select {
		case subscriber.updates <- update:
		default:
			// The next snapshot/status will supersede a queued update for a
			// dashboard client, so dropping here protects the monitor goroutine.
		}
	}
}

func (h *LiveStreamHub) subscribe(provider, canonicalSymbol string) (uint64, <-chan liveStreamUpdate, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.nextID++
	id := h.nextID
	subscriber := &liveStreamSubscriber{
		provider:        strings.ToLower(strings.TrimSpace(provider)),
		canonicalSymbol: strings.ToUpper(strings.TrimSpace(canonicalSymbol)),
		updates:         make(chan liveStreamUpdate, 32),
	}
	h.subscribers[id] = subscriber
	return id, subscriber.updates, func() {
		h.mu.Lock()
		delete(h.subscribers, id)
		h.mu.Unlock()
	}
}

// liveStreamHandler implements the browser-facing Server-Sent Events route.
// The first messages contain current state; subsequent messages are pushed by
// the monitor hub, with periodic heartbeats keeping idle connections alive.
func liveStreamHandler(
	responseWriter http.ResponseWriter,
	request *http.Request,
	hub *LiveStreamHub,
	store *ingestion.LiveMarketSnapshotStore,
	registry *ingestion.LiveMonitorStatusRegistry,
	fallback *ingestion.LiveMonitorStatusStore,
) {
	if hub == nil {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "live stream hub is not configured",
		})
		return
	}
	flusher, ok := responseWriter.(http.Flusher)
	if !ok {
		writeJSON(responseWriter, http.StatusInternalServerError, map[string]string{
			"error": "server-sent events are not supported",
		})
		return
	}

	query := request.URL.Query()
	provider := strings.ToLower(strings.TrimSpace(query.Get("provider")))
	symbol := strings.ToUpper(strings.TrimSpace(query.Get("symbol")))
	responseWriter.Header().Set("Content-Type", "text/event-stream")
	responseWriter.Header().Set("Cache-Control", "no-cache")
	responseWriter.Header().Set("Connection", "keep-alive")
	responseWriter.Header().Set("X-Accel-Buffering", "no")
	responseWriter.WriteHeader(http.StatusOK)

	writeSSE := func(event string, data any) error {
		encoded, err := json.Marshal(data)
		if err != nil {
			return err
		}
		if _, err := fmt.Fprintf(responseWriter, "event: %s\ndata: %s\n\n", event, encoded); err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	if store != nil {
		for _, event := range store.ListFiltered(provider, symbol) {
			if err := writeSSE("snapshot", liveSnapshotResponseFromEvent(event)); err != nil {
				return
			}
		}
	}
	if registry != nil {
		for _, status := range registry.List() {
			if matchesLiveStreamFilter(status.Provider, status.CanonicalSymbol, provider, symbol) {
				if err := writeSSE("status", liveMonitorStatusResponseFrom(status)); err != nil {
					return
				}
			}
		}
	} else if fallback != nil {
		status := fallback.Snapshot()
		if matchesLiveStreamFilter(status.Provider, status.CanonicalSymbol, provider, symbol) {
			if err := writeSSE("status", liveMonitorStatusResponseFrom(status)); err != nil {
				return
			}
		}
	}
	if err := writeSSE("heartbeat", map[string]string{"at": time.Now().UTC().Format(time.RFC3339Nano)}); err != nil {
		return
	}

	_, updates, unsubscribe := hub.subscribe(provider, symbol)
	defer unsubscribe()
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case update := <-updates:
			if err := writeSSE(update.event, update.data); err != nil {
				return
			}
		case now := <-heartbeat.C:
			if err := writeSSE("heartbeat", map[string]string{"at": now.UTC().Format(time.RFC3339Nano)}); err != nil {
				return
			}
		}
	}
}

func matchesLiveStreamFilter(provider, canonicalSymbol, expectedProvider, expectedSymbol string) bool {
	return (expectedProvider == "" || strings.EqualFold(provider, expectedProvider)) &&
		(expectedSymbol == "" || strings.EqualFold(canonicalSymbol, expectedSymbol))
}

func liveSnapshotResponseFromEvent(event ingestion.LiveMarketEvent) liveSnapshotResponse {
	return liveSnapshotResponse{
		CanonicalSymbol:  event.CanonicalSymbol,
		Provider:         string(event.Provider),
		ProviderSymbol:   event.ProviderSymbol,
		EventType:        event.EventType,
		ObservedAt:       event.ObservedAt.UTC().Format(time.RFC3339Nano),
		Price:            requiredNumeric(event.Price),
		Quantity:         requiredNumeric(event.Quantity),
		SourceReceivedAt: event.SourceReceivedAt.UTC().Format(time.RFC3339Nano),
	}
}
