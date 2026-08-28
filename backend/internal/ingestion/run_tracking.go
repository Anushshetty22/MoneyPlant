package ingestion

import (
	// context supplies cancellation for normal work and a fresh bounded context
	// for audit finalization when the work context has already been canceled.
	"context"
	// time bounds the cleanup database call so shutdown cannot hang forever.
	"time"

	"github.com/Anushshetty22/MoneyPlant/backend/internal/database"
	"github.com/jackc/pgx/v5/pgtype"
)

const auditFinalizationTimeout = 5 * time.Second

// completeIngestionRun finalizes an audit row even when the main operation's
// context has been canceled.
//
// Phase 5.2 update: this helper was added because cancellation is normally
// correct for provider and persistence work, but using that same canceled
// context for the final audit UPDATE can leave a run stuck in status=running.
// A fresh short-lived context is used only for this cleanup operation.
func completeIngestionRun(
	ctx context.Context,
	tracker IngestionRunTracker,
	runID int64,
	status string,
	completedAt pgtype.Timestamptz,
	rowsReceived int64,
	rowsInserted int64,
	rowsUpdated int64,
	rowsRejected int64,
	errorMessage *string,
) (database.IngestionRun, error) {
	finalizationContext := ctx
	if ctx.Err() != nil {
		var cancel context.CancelFunc
		finalizationContext, cancel = context.WithTimeout(context.Background(), auditFinalizationTimeout)
		defer cancel()
	}

	return tracker.Complete(
		finalizationContext,
		runID,
		status,
		completedAt,
		rowsReceived,
		rowsInserted,
		rowsUpdated,
		rowsRejected,
		errorMessage,
	)
}
