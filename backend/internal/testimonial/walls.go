package testimonial

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/acidiney/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
)

// WallValues is a validated wall ready to store.
type WallValues struct {
	ProjectID      *string
	Name           string
	Slug           string
	Layout         WallLayout
	Theme          Theme
	ShowRating     bool
	ShowAvatar     bool
	ShowCompany    bool
	ShowVideo      bool
	MaxItems       int
	AllowedOrigins []string
	CreatedBy      *string
}

// CreateWall stores a new draft wall. A wall is never born published: making
// something public is a separate, explicit action gated by testimonial:publish.
func (Store) CreateWall(
	ctx context.Context,
	query db.Querier,
	tenantID, publicID string,
	values WallValues,
) (Wall, error) {
	theme, err := json.Marshal(values.Theme)
	if err != nil {
		return Wall{}, fmt.Errorf("encode wall theme: %w", err)
	}
	origins := values.AllowedOrigins
	if origins == nil {
		origins = []string{}
	}
	var wall Wall
	var themeRaw []byte
	err = query.QueryRow(ctx, `
		INSERT INTO testimonial_walls (
			tenant_id, project_id, public_id, name, slug, layout, theme, show_rating, show_avatar,
			show_company, show_video, max_items, allowed_origins, created_by
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)
		RETURNING `+wallColumns,
		tenantID, values.ProjectID, publicID, values.Name, values.Slug, string(values.Layout),
		string(theme), values.ShowRating, values.ShowAvatar, values.ShowCompany, values.ShowVideo,
		values.MaxItems, origins, values.CreatedBy).
		Scan(&wall.ID, &wall.PublicID, &wall.ProjectID, &wall.Name, &wall.Slug, &wall.Layout,
			&themeRaw, &wall.ShowRating, &wall.ShowAvatar, &wall.ShowCompany, &wall.ShowVideo,
			&wall.MaxItems, &wall.AllowedOrigins, &wall.Status, &wall.PublishedAt,
			&wall.CreatedAt, &wall.UpdatedAt)
	if err != nil {
		return Wall{}, fmt.Errorf("create testimonial wall: %w", err)
	}
	if len(themeRaw) > 0 {
		_ = json.Unmarshal(themeRaw, &wall.Theme)
	}
	return wall, nil
}

// ListWalls returns every wall with its current item count.
func (Store) ListWalls(ctx context.Context, query db.Querier, tenantID string) ([]Wall, error) {
	rows, err := query.Query(ctx, `
		SELECT `+wallColumns+`, (
			SELECT count(*) FROM testimonial_wall_items AS item
			WHERE item.tenant_id = wall.tenant_id AND item.wall_id = wall.id
		)
		FROM testimonial_walls AS wall
		WHERE wall.tenant_id = $1
		ORDER BY wall.created_at DESC
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list testimonial walls: %w", err)
	}
	defer rows.Close()

	walls := []Wall{}
	for rows.Next() {
		var wall Wall
		var themeRaw []byte
		if err := rows.Scan(&wall.ID, &wall.PublicID, &wall.ProjectID, &wall.Name, &wall.Slug,
			&wall.Layout, &themeRaw, &wall.ShowRating, &wall.ShowAvatar, &wall.ShowCompany,
			&wall.ShowVideo, &wall.MaxItems, &wall.AllowedOrigins, &wall.Status, &wall.PublishedAt,
			&wall.CreatedAt, &wall.UpdatedAt, &wall.ItemCount); err != nil {
			return nil, fmt.Errorf("scan testimonial wall: %w", err)
		}
		if len(themeRaw) > 0 {
			_ = json.Unmarshal(themeRaw, &wall.Theme)
		}
		walls = append(walls, wall)
	}
	return walls, rows.Err()
}

// GetWall reads one wall by its internal identifier.
func (Store) GetWall(ctx context.Context, query db.Querier, tenantID, id string) (Wall, error) {
	var wall Wall
	var themeRaw []byte
	err := query.QueryRow(ctx, `
		SELECT `+wallColumns+` FROM testimonial_walls WHERE tenant_id = $1 AND id = $2
	`, tenantID, id).
		Scan(&wall.ID, &wall.PublicID, &wall.ProjectID, &wall.Name, &wall.Slug, &wall.Layout,
			&themeRaw, &wall.ShowRating, &wall.ShowAvatar, &wall.ShowCompany, &wall.ShowVideo,
			&wall.MaxItems, &wall.AllowedOrigins, &wall.Status, &wall.PublishedAt,
			&wall.CreatedAt, &wall.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Wall{}, ErrNotFound
	}
	if err != nil {
		return Wall{}, fmt.Errorf("load testimonial wall: %w", err)
	}
	if len(themeRaw) > 0 {
		_ = json.Unmarshal(themeRaw, &wall.Theme)
	}
	return wall, nil
}

// SetWallItems replaces membership and ordering in one statement pair.
//
// Positions are renumbered from zero in the order supplied. The unique
// constraint on (tenant_id, wall_id, position) is DEFERRABLE INITIALLY DEFERRED
// precisely so this can delete and reinsert without tripping on a transient
// collision half way through.
func (Store) SetWallItems(
	ctx context.Context,
	query db.Querier,
	tenantID, wallID string,
	items []WallItem,
) error {
	if _, err := query.Exec(ctx, `
		DELETE FROM testimonial_wall_items WHERE tenant_id = $1 AND wall_id = $2
	`, tenantID, wallID); err != nil {
		return fmt.Errorf("clear testimonial wall items: %w", err)
	}
	for position, item := range items {
		if _, err := query.Exec(ctx, `
			INSERT INTO testimonial_wall_items (tenant_id, wall_id, testimonial_id, position, pinned)
			VALUES ($1, $2, $3, $4, $5)
		`, tenantID, wallID, item.TestimonialID, position, item.Pinned); err != nil {
			return fmt.Errorf("insert testimonial wall item: %w", err)
		}
	}
	return nil
}

// RemoveTestimonialFromWalls drops a testimonial from every wall and renumbers
// what remains, so archiving or revoking does not leave ghost entries in the
// editor or holes in the ordering.
func (Store) RemoveTestimonialFromWalls(
	ctx context.Context,
	query db.Querier,
	tenantID, testimonialID string,
) error {
	if _, err := query.Exec(ctx, `
		DELETE FROM testimonial_wall_items WHERE tenant_id = $1 AND testimonial_id = $2
	`, tenantID, testimonialID); err != nil {
		return fmt.Errorf("remove testimonial from walls: %w", err)
	}
	if _, err := query.Exec(ctx, `
		UPDATE testimonial_wall_items AS item
		SET position = renumbered.position
		FROM (
			SELECT tenant_id, wall_id, testimonial_id,
			       row_number() OVER (PARTITION BY wall_id ORDER BY position) - 1 AS position
			FROM testimonial_wall_items
			WHERE tenant_id = $1
		) AS renumbered
		WHERE item.tenant_id = renumbered.tenant_id
		  AND item.wall_id = renumbered.wall_id
		  AND item.testimonial_id = renumbered.testimonial_id
		  AND item.position <> renumbered.position
	`, tenantID); err != nil {
		return fmt.Errorf("renumber testimonial wall items: %w", err)
	}
	return nil
}

// PublishableCard is a testimonial joined with everything the publishability
// predicate needs, ready to be projected into a snapshot.
type PublishableCard struct {
	Testimonial
	MediaReady    bool     `json:"media_ready"`
	HasVideo      bool     `json:"has_video"`
	VideoReviewed bool     `json:"video_reviewed"`
	AvatarURL     string   `json:"avatar_url,omitempty"`
	VideoURL      string   `json:"video_url,omitempty"`
	TagNames      []string `json:"tag_names,omitempty"`
	Position      int      `json:"position"`
}

// LoadWallCards reads wall membership in position order with the media state
// each card needs. Media that is not both safety-clean and transcoded makes a
// card unpublishable rather than silently rendering a broken asset.
func (Store) LoadWallCards(
	ctx context.Context,
	query db.Querier,
	tenantID, wallID string,
) ([]PublishableCard, error) {
	rows, err := query.Query(ctx, `
		SELECT t.id, t.public_id, t.consented_text, t.quote, t.headline, t.author_name,
		       t.author_role, t.author_company, t.author_company_url, t.rating, t.status,
		       t.consent_state, t.consent_scopes, item.position,
		       COALESCE(avatar.public_id, ''), COALESCE(avatar.content_hash, ''),
		       COALESCE(video.public_id, ''), COALESCE(video.content_hash, ''),
		       COALESCE(bool_and(
		           media.safety_status = 'safe' AND media.transcode_status = 'ready'
		       ) FILTER (WHERE media.id IS NOT NULL), true),
		       COALESCE(array_agg(DISTINCT tag.name::text) FILTER (WHERE tag.name IS NOT NULL), '{}')
		FROM testimonial_wall_items AS item
		JOIN testimonials AS t ON t.tenant_id = item.tenant_id AND t.id = item.testimonial_id
		LEFT JOIN testimonial_media AS media
		       ON media.tenant_id = t.tenant_id AND media.testimonial_id = t.id
		LEFT JOIN testimonial_media AS avatar
		       ON avatar.tenant_id = t.tenant_id AND avatar.testimonial_id = t.id
		      AND avatar.role = 'avatar'
		LEFT JOIN testimonial_media AS video
		       ON video.tenant_id = t.tenant_id AND video.testimonial_id = t.id
		      AND video.role = 'video'
		LEFT JOIN testimonial_tags AS link
		       ON link.tenant_id = t.tenant_id AND link.testimonial_id = t.id
		LEFT JOIN tags AS tag ON tag.tenant_id = link.tenant_id AND tag.id = link.tag_id
		WHERE item.tenant_id = $1 AND item.wall_id = $2
		GROUP BY t.id, t.public_id, t.consented_text, t.quote, t.headline, t.author_name,
		         t.author_role, t.author_company, t.author_company_url, t.rating, t.status,
		         t.consent_state, t.consent_scopes, item.position, avatar.public_id,
		         avatar.content_hash, video.public_id, video.content_hash
		ORDER BY item.position
	`, tenantID, wallID)
	if err != nil {
		return nil, fmt.Errorf("load testimonial wall cards: %w", err)
	}
	defer rows.Close()

	cards := []PublishableCard{}
	for rows.Next() {
		var card PublishableCard
		var scopes []byte
		var author authorColumns
		var avatarID, avatarHash, videoID, videoHash string
		if err := rows.Scan(
			&card.ID, &card.PublicID, &card.ConsentedText, &card.Quote, &card.Headline,
			&author.name, &author.role, &author.company, &author.companyURL,
			&card.Rating, &card.Status, &card.ConsentState, &scopes, &card.Position,
			&avatarID, &avatarHash, &videoID, &videoHash, &card.MediaReady, &card.TagNames,
		); err != nil {
			return nil, fmt.Errorf("scan testimonial wall card: %w", err)
		}
		card.Author = author.details()
		if len(scopes) > 0 {
			_ = json.Unmarshal(scopes, &card.Scopes)
		}
		if avatarID != "" && avatarHash != "" {
			card.AvatarURL = MediaPath(avatarID, avatarHash, "jpg")
		}
		if videoID != "" && videoHash != "" {
			card.HasVideo = true
			card.VideoURL = MediaPath(videoID, videoHash, "mp4")
		}
		// Video review is recorded on the media row in a later slice; until then a
		// wall containing video cannot be published, which is the safe direction.
		card.VideoReviewed = false
		cards = append(cards, card)
	}
	return cards, rows.Err()
}

// MediaPath builds the stable, content-addressed public path for an asset.
//
// The snapshot embeds this path and never a presigned URL: a presigned URL
// expires and the snapshot does not, so a cached wall would show broken images
// once the signature aged out. The API resolves this path and issues a
// short-lived redirect per request instead.
func MediaPath(publicID, contentHash, extension string) string {
	return fmt.Sprintf("/v1/public/media/%s/%s.%s", publicID, contentHash, extension)
}

// BuildSnapshotPayload projects publishable cards into the payload the embed
// consumes. Cards that fail the predicate are dropped rather than rendered
// partially, and every surviving card is projected through its consent scopes.
func BuildSnapshotPayload(wall Wall, cards []PublishableCard, now time.Time) SnapshotPayload {
	payload := SnapshotPayload{
		SchemaVersion: SnapshotSchemaVersion,
		WallPublicID:  wall.PublicID,
		Name:          wall.Name,
		Layout:        wall.Layout,
		Theme:         wall.Theme,
		ShowRating:    wall.ShowRating,
		ShowAvatar:    wall.ShowAvatar,
		ShowCompany:   wall.ShowCompany,
		ShowVideo:     wall.ShowVideo,
		Cards:         []SnapshotCard{},
		GeneratedAt:   now.UTC(),
	}
	for _, card := range cards {
		if len(payload.Cards) >= wall.MaxItems {
			break
		}
		state := card.Publishability(card.MediaReady, card.HasVideo, card.VideoReviewed)
		if !state.Publishable() {
			continue
		}
		details := AuthorDetails{
			Name:       card.Author.Name,
			Role:       card.Author.Role,
			Company:    card.Author.Company,
			CompanyURL: card.Author.CompanyURL,
			AvatarURL:  card.AvatarURL,
			VideoURL:   card.VideoURL,
		}
		rendered := SnapshotCard{
			ID:     card.PublicID,
			Quote:  card.Quote,
			Author: details.Project(card.Scopes),
			Tags:   card.TagNames,
		}
		if card.Headline != nil {
			rendered.Headline = *card.Headline
		}
		if wall.ShowRating {
			rendered.Rating = card.Rating
		}
		if !wall.ShowAvatar {
			rendered.Author.AvatarURL = ""
		}
		if !wall.ShowCompany {
			rendered.Author.Company = ""
			rendered.Author.CompanyURL = ""
		}
		if !wall.ShowVideo {
			rendered.Author.VideoURL = ""
		}
		payload.Cards = append(payload.Cards, rendered)
	}
	return payload
}

// PublishSnapshot writes a new immutable version, makes it current, and marks
// the wall published. Old versions are retained: they are what makes rollback
// trivial and what records exactly what was publicly visible when, which matters
// if consent is ever disputed.
func (Store) PublishSnapshot(
	ctx context.Context,
	query db.Querier,
	tenantID, wallID string,
	payload SnapshotPayload,
	memberID *string,
	now time.Time,
) (Snapshot, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Snapshot{}, fmt.Errorf("encode snapshot payload: %w", err)
	}
	etag := hashPayload(encoded)

	if _, err := query.Exec(ctx, `
		UPDATE testimonial_wall_snapshots SET is_current = false
		WHERE tenant_id = $1 AND wall_id = $2 AND is_current
	`, tenantID, wallID); err != nil {
		return Snapshot{}, fmt.Errorf("retire current snapshot: %w", err)
	}

	var version int
	if err := query.QueryRow(ctx, `
		INSERT INTO testimonial_wall_snapshots (
			tenant_id, wall_id, version, payload, etag, is_current, is_stale, item_count,
			published_at, built_by
		)
		SELECT $1, $2, COALESCE(max(version), 0) + 1, $3, $4, true, false, $5, $6, $7
		FROM testimonial_wall_snapshots WHERE tenant_id = $1 AND wall_id = $2
		RETURNING version
	`, tenantID, wallID, string(encoded), etag, len(payload.Cards), now, memberID).Scan(&version); err != nil {
		return Snapshot{}, fmt.Errorf("insert snapshot: %w", err)
	}

	if _, err := query.Exec(ctx, `
		UPDATE testimonial_walls SET status = 'published', published_at = $3
		WHERE tenant_id = $1 AND id = $2
	`, tenantID, wallID, now); err != nil {
		return Snapshot{}, fmt.Errorf("mark wall published: %w", err)
	}

	return Snapshot{
		WallID:      wallID,
		Version:     version,
		Payload:     payload,
		ETag:        etag,
		IsCurrent:   true,
		ItemCount:   len(payload.Cards),
		PublishedAt: now,
	}, nil
}

// LoadCurrentSnapshot reads the published payload for a wall.
func (Store) LoadCurrentSnapshot(
	ctx context.Context,
	query db.Querier,
	tenantID, wallID string,
) (Snapshot, error) {
	var snapshot Snapshot
	var encoded []byte
	err := query.QueryRow(ctx, `
		SELECT wall_id, version, payload, etag, is_current, is_stale, item_count, published_at
		FROM testimonial_wall_snapshots
		WHERE tenant_id = $1 AND wall_id = $2 AND is_current
	`, tenantID, wallID).Scan(&snapshot.WallID, &snapshot.Version, &encoded, &snapshot.ETag,
		&snapshot.IsCurrent, &snapshot.IsStale, &snapshot.ItemCount, &snapshot.PublishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Snapshot{}, ErrNotFound
	}
	if err != nil {
		return Snapshot{}, fmt.Errorf("load current snapshot: %w", err)
	}
	if err := json.Unmarshal(encoded, &snapshot.Payload); err != nil {
		return Snapshot{}, fmt.Errorf("decode snapshot payload: %w", err)
	}
	return snapshot, nil
}

// RevokedCardIDs returns the public identifiers on a wall whose consent is no
// longer granted.
//
// This is the bounded fallback for a stale snapshot: filtering a stored payload
// through a short indexed lookup, rather than re-projecting the whole wall live
// on every request until the rebuild lands.
func (Store) RevokedCardIDs(
	ctx context.Context,
	query db.Querier,
	tenantID, wallID string,
) (map[string]struct{}, error) {
	rows, err := query.Query(ctx, `
		SELECT t.public_id
		FROM testimonial_wall_items AS item
		JOIN testimonials AS t ON t.tenant_id = item.tenant_id AND t.id = item.testimonial_id
		WHERE item.tenant_id = $1 AND item.wall_id = $2
		  AND (t.consent_state <> 'granted' OR t.status <> 'approved')
	`, tenantID, wallID)
	if err != nil {
		return nil, fmt.Errorf("load revoked wall cards: %w", err)
	}
	defer rows.Close()
	revoked := map[string]struct{}{}
	for rows.Next() {
		var publicID string
		if err := rows.Scan(&publicID); err != nil {
			return nil, fmt.Errorf("scan revoked wall card: %w", err)
		}
		revoked[publicID] = struct{}{}
	}
	return revoked, rows.Err()
}
