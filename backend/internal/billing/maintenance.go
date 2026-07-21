package billing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/email"
)

type PrefixDeleter interface {
	RemovePrefix(context.Context, string) error
}

type DeletionExecutor struct {
	Store    DeletionStore
	Provider Provider
	Storage  PrefixDeleter
	Logger   *slog.Logger
}

type DeletionStore interface {
	Get(context.Context, string) (Tenant, error)
	DueDeletions(context.Context) ([]Tenant, error)
	UnverifiedTenants(context.Context) ([]Tenant, error)
	PurgeUnverified(context.Context, string) error
	DeleteTenant(context.Context, Tenant, string) error
}

func (e DeletionExecutor) ForceDelete(ctx context.Context, tenantID, platformAdminID string) error {
	current, err := e.Store.Get(ctx, tenantID)
	if err != nil {
		return err
	}
	return e.delete(ctx, current, platformAdminID)
}

func (e DeletionExecutor) Execute(ctx context.Context) error {
	due, err := e.Store.DueDeletions(ctx)
	if err != nil {
		return err
	}
	var joined error
	for _, current := range due {
		if err := e.delete(ctx, current, ""); err != nil {
			joined = errors.Join(joined, fmt.Errorf("delete tenant %s: %w", current.ID, err))
		}
	}
	unverified, err := e.Store.UnverifiedTenants(ctx)
	if err != nil {
		return errors.Join(joined, err)
	}
	for _, current := range unverified {
		if err := e.Storage.RemovePrefix(ctx, "tenants/"+current.ID+"/"); err != nil {
			joined = errors.Join(joined, fmt.Errorf("delete unverified tenant storage %s: %w", current.ID, err))
			continue
		}
		if err := e.Store.PurgeUnverified(ctx, current.ID); err != nil {
			joined = errors.Join(joined, err)
		}
	}
	return joined
}

func (e DeletionExecutor) delete(ctx context.Context, current Tenant, platformAdminID string) error {
	if current.StripeSubscriptionID != "" {
		if err := e.Provider.CancelSubscription(ctx, current.StripeSubscriptionID); err != nil {
			return err
		}
	}
	if err := e.Storage.RemovePrefix(ctx, "tenants/"+current.ID+"/"); err != nil {
		return err
	}
	return e.Store.DeleteTenant(ctx, current, platformAdminID)
}

func (e DeletionExecutor) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if err := e.Execute(ctx); err != nil && e.Logger != nil {
		e.Logger.ErrorContext(ctx, "tenant deletion executor failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := e.Execute(ctx); err != nil && e.Logger != nil {
				e.Logger.ErrorContext(ctx, "tenant deletion executor failed", "error", err)
			}
		}
	}
}

type Dunning struct {
	Store    DunningStore
	Email    email.Enqueuer
	AuthFrom string
	Logger   *slog.Logger
	Now      func() time.Time
}

type DunningStore interface {
	TrialsForDunning(context.Context) ([]Tenant, error)
	SuspendExpiredUnpaid(context.Context, string) error
	MemberEmails(context.Context, string) ([]string, error)
}

func (d Dunning) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

func (d Dunning) Execute(ctx context.Context) error {
	tenants, err := d.Store.TrialsForDunning(ctx)
	if err != nil {
		return err
	}
	var joined error
	for _, current := range tenants {
		if current.TrialEndsAt == nil {
			continue
		}
		remaining := current.TrialEndsAt.Sub(d.now())
		days := int((remaining + 24*time.Hour - time.Nanosecond) / (24 * time.Hour))
		if remaining <= 0 {
			days = 0
		}
		if days != 0 && days != 1 && days != 3 {
			continue
		}
		addresses, err := d.Store.MemberEmails(ctx, current.ID)
		if err != nil {
			joined = errors.Join(joined, err)
			continue
		}
		addresses = appendUnique(addresses, current.BillingEmail)
		for _, address := range addresses {
			message := email.Message{
				From: d.AuthFrom, To: address, Subject: "Your Fidibeki trial is ending",
				Text:           fmt.Sprintf("Your %s trial ends in %d day(s). Add a payment method in billing settings to keep the account active.", current.Name, days),
				IdempotencyKey: fmt.Sprintf("trial-dunning:%s:%s:%s:%d", current.ID, address, current.TrialEndsAt.UTC().Format("2006-01-02"), days),
			}
			if err := d.Email.Enqueue(ctx, message); err != nil {
				joined = errors.Join(joined, err)
			}
		}
		if remaining <= 0 {
			if err := d.Store.SuspendExpiredUnpaid(ctx, current.ID); err != nil {
				joined = errors.Join(joined, err)
			}
		}
	}
	return joined
}

func appendUnique(values []string, candidate string) []string {
	if candidate == "" {
		return values
	}
	for _, value := range values {
		if value == candidate {
			return values
		}
	}
	return append(values, candidate)
}

func (d Dunning) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = 24 * time.Hour
	}
	if err := d.Execute(ctx); err != nil && d.Logger != nil {
		d.Logger.ErrorContext(ctx, "trial dunning failed", "error", err)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := d.Execute(ctx); err != nil && d.Logger != nil {
				d.Logger.ErrorContext(ctx, "trial dunning failed", "error", err)
			}
		}
	}
}
