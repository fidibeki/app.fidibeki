package triage

import (
	"context"
	"errors"
	"fmt"

	"github.com/fidibeki/app.fidibeki/backend/internal/db"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

var (
	ErrTagNotFound = errors.New("tag not found")
	ErrTagConflict = errors.New("tag already exists")
)

type Store struct{}

func (Store) ListTags(
	ctx context.Context,
	query db.Querier,
	tenantID string,
) ([]Tag, error) {
	rows, err := query.Query(ctx, `
		SELECT id, name::text, color
		FROM tags
		WHERE tenant_id = $1
		ORDER BY lower(name::text), id
	`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("list tags: %w", err)
	}
	defer rows.Close()
	tags := make([]Tag, 0)
	for rows.Next() {
		var tag Tag
		if err := rows.Scan(&tag.ID, &tag.Name, &tag.Color); err != nil {
			return nil, fmt.Errorf("scan tag: %w", err)
		}
		tags = append(tags, tag)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tags: %w", err)
	}
	return tags, nil
}

func (Store) CreateTag(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	name string,
	color string,
) (Tag, error) {
	var created Tag
	err := query.QueryRow(ctx, `
		INSERT INTO tags (tenant_id, name, color)
		VALUES ($1, $2, $3)
		RETURNING id, name::text, color
	`, tenantID, name, color).Scan(&created.ID, &created.Name, &created.Color)
	if tagConflict(err) {
		return Tag{}, ErrTagConflict
	}
	if err != nil {
		return Tag{}, fmt.Errorf("create tag: %w", err)
	}
	return created, nil
}

func (Store) UpdateTag(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	tagID string,
	name *string,
	color *string,
) (Tag, error) {
	var updated Tag
	err := query.QueryRow(ctx, `
		UPDATE tags
		SET name = COALESCE($3, name), color = COALESCE($4, color)
		WHERE tenant_id = $1 AND id = $2
		RETURNING id, name::text, color
	`, tenantID, tagID, name, color).Scan(&updated.ID, &updated.Name, &updated.Color)
	if errors.Is(err, pgx.ErrNoRows) {
		return Tag{}, ErrTagNotFound
	}
	if tagConflict(err) {
		return Tag{}, ErrTagConflict
	}
	if err != nil {
		return Tag{}, fmt.Errorf("update tag: %w", err)
	}
	return updated, nil
}

func (Store) DeleteTag(
	ctx context.Context,
	query db.Querier,
	tenantID string,
	tagID string,
) error {
	result, err := query.Exec(ctx, `DELETE FROM tags WHERE tenant_id = $1 AND id = $2`, tenantID, tagID)
	if err != nil {
		return fmt.Errorf("delete tag: %w", err)
	}
	if result.RowsAffected() == 0 {
		return ErrTagNotFound
	}
	return nil
}

func tagConflict(err error) bool {
	var postgresError *pgconn.PgError
	return errors.As(err, &postgresError) && postgresError.Code == "23505"
}
