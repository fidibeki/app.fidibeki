package testimonial

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/fidibeki/app.fidibeki/backend/internal/tenant"
	"github.com/jackc/pgx/v5"
)

// ErrPublicAccessDenied is returned for every failure on the public path:
// unknown wall, unpublished wall, suspended tenant, expired trial, missing
// entitlement. The handler answers all of them with 404. Whether a wall exists
// under a suspended workspace is not information an anonymous caller is owed.
var ErrPublicAccessDenied = errors.New("testimonial wall is not publicly available")

// PublicWall is everything the public endpoint needs, resolved in one query
// before any tenant context exists.
type PublicWall struct {
	TenantID       string
	WallID         string
	PublicID       string
	AllowedOrigins []string
}

// PublicAuthorizer resolves a wall public identifier to a workspace and decides
// whether it may be served.
//
// It mirrors widgetapi.Authorizer deliberately. The widget refuses to serve
// configuration for a suspended or lapsed workspace; without the same check
// here, a suspended tenant would keep serving testimonials from our domain
// indefinitely, which is both a billing hole and, for a workspace suspended over
// abuse, a moderation hole.
type PublicAuthorizer struct {
	// Database must be the admin pool: this query runs before app.tenant_id is
	// set, so the row-level policies on fidibeki_app would hide the row.
	Database db.Querier
	Now      func() time.Time
}

// Authorize resolves the wall and asserts the workspace may publish.
func (a PublicAuthorizer) Authorize(ctx context.Context, publicID string) (PublicWall, error) {
	if publicID == "" {
		return PublicWall{}, ErrPublicAccessDenied
	}
	var resolved PublicWall
	var status string
	var trialEndsAt *time.Time
	var deletionRequestedAt *time.Time
	var wallStatus string
	err := a.Database.QueryRow(ctx, `
		SELECT wall.tenant_id, wall.id, wall.public_id, wall.allowed_origins, wall.status,
		       tenant.status, tenant.trial_ends_at, tenant.deletion_requested_at
		FROM testimonial_walls AS wall
		JOIN tenants AS tenant ON tenant.id = wall.tenant_id
		WHERE wall.public_id = $1
	`, publicID).Scan(&resolved.TenantID, &resolved.WallID, &resolved.PublicID,
		&resolved.AllowedOrigins, &wallStatus, &status, &trialEndsAt, &deletionRequestedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return PublicWall{}, ErrPublicAccessDenied
	}
	if err != nil {
		return PublicWall{}, fmt.Errorf("resolve public testimonial wall: %w", err)
	}
	if wallStatus != string(WallPublished) {
		return PublicWall{}, ErrPublicAccessDenied
	}

	now := time.Now()
	if a.Now != nil {
		now = a.Now()
	}
	// A workspace that has requested deletion keeps serving until the deletion
	// runs, matching how the widget treats the same state: pulling a customer's
	// testimonials off their homepage the moment they click cancel would be a
	// surprise, not a safeguard.
	active := status == string(tenant.StatusActive) || deletionRequestedAt != nil
	validTrial := status == string(tenant.StatusTrial) && trialEndsAt != nil && trialEndsAt.After(now)
	if !active && !validTrial {
		return PublicWall{}, ErrPublicAccessDenied
	}
	return resolved, nil
}

// OriginAllowed reports whether a referring origin is on the wall's allowlist.
//
// This is casual reuse prevention, not access control, and the backoffice says
// so next to the field. Wall data is public by definition: anyone who knows the
// identifier can fetch it directly with no Origin header at all. Presenting this
// as a security boundary would be a lie, and storing it without enforcing it
// would be worse — a customer would believe it protected them.
func OriginAllowed(origin string, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	candidate := normaliseOrigin(origin)
	if candidate == "" {
		// A direct fetch carries no origin. Allowing it is honest: the allowlist
		// cannot stop it, and pretending otherwise would invite someone to rely
		// on it.
		return true
	}
	for _, entry := range allowed {
		if normaliseOrigin(entry) == candidate {
			return true
		}
	}
	return false
}

func normaliseOrigin(value string) string {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" || value == "null" {
		return ""
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" {
		return ""
	}
	return parsed.Scheme + "://" + parsed.Host
}
