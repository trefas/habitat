package postgres

import (
	"context"

	"habitat/internal/domain"
)

// ListNotifications возвращает уведомления пользователя.
func (s *Storage) ListNotifications(ctx context.Context, userID string, unreadOnly bool,
	limit, offset int) ([]domain.Notification, int, error) {

	var total int
	if err := s.pool.QueryRow(ctx, `
		SELECT count(*)::int FROM notifications
		WHERE user_id = $1::uuid AND (NOT $2::boolean OR read_at IS NULL)`,
		userID, unreadOnly).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := s.pool.Query(ctx, `
		SELECT id::text, user_id::text, kind,
		       entity_id::text, share_id::text, actor_id::text,
		       payload, read_at, created_at
		FROM notifications
		WHERE user_id = $1::uuid AND (NOT $2::boolean OR read_at IS NULL)
		ORDER BY created_at DESC, id
		LIMIT $3 OFFSET $4`, userID, unreadOnly, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]domain.Notification, 0, limit)
	for rows.Next() {
		var n domain.Notification
		if err := rows.Scan(&n.ID, &n.UserID, &n.Kind, &n.EntityID, &n.ShareID, &n.ActorID,
			&n.Payload, &n.ReadAt, &n.CreatedAt); err != nil {
			return nil, 0, err
		}
		list = append(list, n)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// MarkNotificationRead отмечает уведомление прочитанным. Идемпотентно.
func (s *Storage) MarkNotificationRead(ctx context.Context, userID, notificationID string) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE notifications
		SET read_at = COALESCE(read_at, now())
		WHERE id = $1::uuid AND user_id = $2::uuid`, notificationID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	return nil
}
