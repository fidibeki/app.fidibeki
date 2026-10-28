package widgetapi

import (
	"context"
	"errors"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/project"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
)

var ErrAccessDenied = errors.New("widget access denied")

type ProjectAuthorizer interface {
	Authorize(context.Context, string, string) (project.PublicConfiguration, error)
}

type Authorizer struct {
	Store    ConfigurationStore
	Database db.Querier
	Now      func() time.Time
}

func (a Authorizer) Authorize(
	ctx context.Context,
	publicKey string,
	origin string,
) (project.PublicConfiguration, error) {
	if publicKey == "" || !ValidOrigin(origin) {
		return project.PublicConfiguration{}, ErrAccessDenied
	}
	configuration, err := a.Store.LoadPublicConfiguration(ctx, a.Database, publicKey)
	if errors.Is(err, project.ErrNotFound) {
		return project.PublicConfiguration{}, ErrAccessDenied
	}
	if err != nil {
		return project.PublicConfiguration{}, err
	}
	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	active := configuration.Status == tenant.StatusActive || configuration.DeletionRequestedAt != nil
	validTrial := configuration.Status == tenant.StatusTrial && configuration.TrialEndsAt != nil &&
		configuration.TrialEndsAt.After(now)
	if (!active && !validTrial) || !OriginAllowed(origin, configuration.AllowedDomains) {
		return project.PublicConfiguration{}, ErrAccessDenied
	}
	return configuration, nil
}
