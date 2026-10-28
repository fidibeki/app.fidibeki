package testimonial

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
)

const (
	// scoringBatchSize bounds one sweep so a workspace with a large backlog does
	// not hold a connection for minutes on its first run.
	scoringBatchSize = 200
	// scoringMaxBatches bounds a single sweep overall; whatever is left is picked
	// up on the next tick rather than starved out by one enormous tenant.
	scoringMaxBatches = 25
	// decayHalfLifeDays shapes how quickly a candidate loses ground to newer
	// material. Ninety days is long enough that a good quote from last quarter
	// still surfaces, short enough that the queue does not calcify.
	decayHalfLifeDays = 90.0
)

// ScoringOperations is the persistence the sweep needs.
type ScoringOperations interface {
	PendingScoringWork(context.Context, db.Querier, int, int) ([]ScoringWork, error)
	UpsertCandidate(context.Context, db.Querier, string, string, string, Assessment, float64, time.Time) error
	RefreshDecayedScores(context.Context, db.Querier, time.Time) (int64, error)
}

// ScoringWork is one feedback awaiting a score, joined with the signals the
// engine reads.
type ScoringWork struct {
	TenantID         string
	ProjectID        string
	FeedbackID       string
	Text             string
	Rating           *int
	FeedbackStatus   string
	ModerationStatus string
	SentimentScore   *float64
	ModerationFlags  []string
	AuthorReachable  bool
	Language         string
	SupportedLocales []string
	HasTestimonial   bool
}

// Input projects the row into what the pure scoring function consumes.
func (w ScoringWork) Input() Input {
	return Input{
		Text:             w.Text,
		Rating:           w.Rating,
		FeedbackStatus:   w.FeedbackStatus,
		ModerationStatus: w.ModerationStatus,
		SentimentScore:   w.SentimentScore,
		ModerationFlags:  w.ModerationFlags,
		AuthorReachable:  w.AuthorReachable,
		Language:         w.Language,
		SupportedLocales: w.SupportedLocales,
		HasTestimonial:   w.HasTestimonial,
	}
}

// ScoringMaintainer keeps the Suggested queue populated.
//
// Without it every other part of this feature works and the queue is
// permanently empty: the scoring function is pure, the store can persist a
// result, and the backoffice can render one, but nothing connects a feedback to
// a candidate row.
//
// It runs as a periodic sweep rather than a per-feedback job because scoring is
// cheap, the inputs change underneath it (a feedback can be marked spam, or
// moderation can complete late), and a sweep is naturally idempotent.
type ScoringMaintainer struct {
	Store    ScoringOperations
	Database db.Querier
	Logger   *slog.Logger
	Now      func() time.Time
	// BatchSize and MaxBatches bound one sweep. Zero means the defaults above.
	BatchSize  int
	MaxBatches int
}

// MaintainOnce scores everything currently eligible for a score, then refreshes
// the decay applied to existing candidates.
func (m ScoringMaintainer) MaintainOnce(ctx context.Context) error {
	now := m.now()
	batchSize, maxBatches := m.BatchSize, m.MaxBatches
	if batchSize <= 0 {
		batchSize = scoringBatchSize
	}
	if maxBatches <= 0 {
		maxBatches = scoringMaxBatches
	}

	scored := 0
	for batch := range maxBatches {
		work, err := m.Store.PendingScoringWork(ctx, m.Database, ScoreVersion, batchSize)
		if err != nil {
			return fmt.Errorf("load testimonial scoring work: %w", err)
		}
		if len(work) == 0 {
			break
		}
		for _, item := range work {
			assessment := Score(item.Input())
			if err := m.Store.UpsertCandidate(ctx, m.Database, item.TenantID, item.FeedbackID,
				item.ProjectID, assessment, decayedScore(assessment.BaseScore, now, now), now); err != nil {
				return fmt.Errorf("store testimonial candidate: %w", err)
			}
			scored++
		}
		if len(work) < batchSize {
			break
		}
		if batch == maxBatches-1 {
			// Say so rather than looking like the queue is empty. A silently
			// truncated sweep reads as "everything is scored".
			m.logger().InfoContext(ctx, "testimonial scoring sweep hit its batch ceiling",
				"scored", scored, "batch_size", batchSize, "max_batches", maxBatches)
		}
	}

	refreshed, err := m.Store.RefreshDecayedScores(ctx, m.Database, now)
	if err != nil {
		return fmt.Errorf("refresh testimonial candidate decay: %w", err)
	}
	if scored > 0 || refreshed > 0 {
		m.logger().InfoContext(ctx, "testimonial scoring sweep complete",
			"scored", scored, "decay_refreshed", refreshed)
	}
	return nil
}

// Run sweeps on an interval until the context is cancelled.
func (m ScoringMaintainer) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 15 * time.Minute
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		if err := m.MaintainOnce(ctx); err != nil && ctx.Err() == nil {
			m.logger().ErrorContext(ctx, "testimonial scoring sweep failed", "error", err)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
	}
}

func (m ScoringMaintainer) now() time.Time {
	if m.Now != nil {
		return m.Now()
	}
	return time.Now().UTC()
}

func (m ScoringMaintainer) logger() *slog.Logger {
	if m.Logger != nil {
		return m.Logger
	}
	return slog.Default()
}

// decayedScore applies exponential decay by age.
//
// It is materialised rather than computed at read time because an index on
// base_score cannot satisfy an ORDER BY on the decayed value, which would turn
// every load of the Suggested tab into a scan and sort of the whole eligible
// set.
func decayedScore(base float64, createdAt, now time.Time) float64 {
	ageDays := now.Sub(createdAt).Hours() / 24
	if ageDays < 0 {
		ageDays = 0
	}
	return clamp(base * math.Exp(-ageDays/decayHalfLifeDays))
}
