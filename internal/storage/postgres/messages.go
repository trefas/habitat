package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"

	"habitat/internal/domain"
)

// messageAccessWhere ограничивает выборку сообщениями, доступными пользователю $1.
// $2 — коробка: inbox, sent или all.
const messageAccessWhere = `
	e.type_code = 'message'
	AND e.is_archived = FALSE
	AND (
		($2 = 'sent' AND e.owner_id = $1::uuid)
		OR ($2 = 'inbox' AND EXISTS (
			SELECT 1 FROM entity_shares s
			WHERE s.entity_id = e.id
			  AND s.grantee_id = $1::uuid
			  AND s.mode = 'direct'
			  AND s.revoked_at IS NULL
			  AND (s.expires_at IS NULL OR s.expires_at > now())
		))
		OR ($2 = 'all' AND (
			e.owner_id = $1::uuid
			OR EXISTS (
				SELECT 1 FROM entity_shares s
				WHERE s.entity_id = e.id
				  AND s.grantee_id = $1::uuid
				  AND s.mode = 'direct'
				  AND s.revoked_at IS NULL
				  AND (s.expires_at IS NULL OR s.expires_at > now())
			)
		))
	)`

const messageSelect = `
	SELECT e.id::text, e.owner_id::text, u.email, u.display_name, e.title, m.text,
	       (e.owner_id = $1::uuid) AS is_owner,
	       mr.read_at,
	       CASE WHEN e.owner_id = $1::uuid THEN (
	           SELECT count(*)::int FROM entity_shares s
	           WHERE s.entity_id = e.id AND s.mode = 'direct' AND s.revoked_at IS NULL
	             AND (s.expires_at IS NULL OR s.expires_at > now())
	       ) ELSE 0 END AS recipients_count,
	       CASE WHEN e.owner_id = $1::uuid THEN (
	           SELECT count(*)::int FROM message_reads r WHERE r.entity_id = e.id
	       ) ELSE 0 END AS read_count,
	       e.created_at, e.updated_at
	FROM entities e
	JOIN messages m ON m.entity_id = e.id
	JOIN users u ON u.id = e.owner_id
	LEFT JOIN message_reads mr ON mr.entity_id = e.id AND mr.user_id = $1::uuid`

func scanMessage(row pgx.Row, m *domain.Message) error {
	return row.Scan(&m.ID, &m.OwnerID, &m.OwnerEmail, &m.OwnerDisplayName, &m.Title, &m.Text,
		&m.IsOwner, &m.ReadAt, &m.RecipientsCount, &m.ReadCount, &m.CreatedAt, &m.UpdatedAt)
}

// CreateMessage создаёт сообщение, выдаёт доступ получателям и создаёт уведомления.
func (s *Storage) CreateMessage(ctx context.Context, ownerID, text string,
	recipients []domain.MessageRecipient) (*domain.Message, error) {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	var msg domain.Message
	err = tx.QueryRow(ctx, `
		INSERT INTO entities (type_code, owner_id, title)
		VALUES ('message', $1::uuid, left($2, 120))
		RETURNING id::text, title, created_at, updated_at`,
		ownerID, text).Scan(&msg.ID, &msg.Title, &msg.CreatedAt, &msg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	if _, err := tx.Exec(ctx,
		`INSERT INTO messages (entity_id, text) VALUES ($1::uuid, $2)`, msg.ID, text); err != nil {
		return nil, err
	}

	payload := messagePayload(text)
	for _, r := range recipients {
		var shareID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO entity_shares (entity_id, grantor_id, grantee_id, mode, permission)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'direct', 'read')
			RETURNING id::text`, msg.ID, ownerID, r.UserID).Scan(&shareID); err != nil {
			return nil, err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO notifications (user_id, kind, entity_id, share_id, actor_id, payload)
			VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5::uuid, $6::jsonb)`,
			r.UserID, domain.NotificationShareReceived, msg.ID, shareID, ownerID, payload); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}

	msg.OwnerID = ownerID
	msg.Text = text
	msg.IsOwner = true
	msg.RecipientsCount = len(recipients)
	return &msg, nil
}

// ListMessages возвращает сообщения пользователя и общее количество.
func (s *Storage) ListMessages(ctx context.Context, userID, box string,
	limit, offset int) ([]domain.Message, int, error) {

	var total int
	if err := s.pool.QueryRow(ctx,
		`SELECT count(*)::int FROM entities e WHERE `+messageAccessWhere,
		userID, box).Scan(&total); err != nil {
		return nil, 0, err
	}

	q := messageSelect + `
		WHERE ` + messageAccessWhere + `
		ORDER BY e.created_at DESC, e.id
		LIMIT $3 OFFSET $4`

	rows, err := s.pool.Query(ctx, q, userID, box, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	list := make([]domain.Message, 0, limit)
	for rows.Next() {
		var m domain.Message
		if err := scanMessage(rows, &m); err != nil {
			return nil, 0, err
		}
		list = append(list, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// GetMessage возвращает сообщение, доступное пользователю.
func (s *Storage) GetMessage(ctx context.Context, userID, id string) (*domain.Message, error) {
	q := messageSelect + `
		WHERE e.id = $2::uuid
		  AND e.type_code = 'message'
		  AND e.is_archived = FALSE
		  AND (
			e.owner_id = $1::uuid
			OR EXISTS (
				SELECT 1 FROM entity_shares s
				WHERE s.entity_id = e.id
				  AND s.grantee_id = $1::uuid
				  AND s.mode = 'direct'
				  AND s.revoked_at IS NULL
				  AND (s.expires_at IS NULL OR s.expires_at > now())
			)
		  )`

	var m domain.Message
	if err := scanMessage(s.pool.QueryRow(ctx, q, userID, id), &m); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &m, nil
}

// ListMessageRecipients возвращает получателей сообщения и факты прочтения.
// Метод доступен только владельцу.
func (s *Storage) ListMessageRecipients(ctx context.Context, ownerID, messageID string) ([]domain.MessageRecipient, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id::text, u.email, u.display_name, r.read_at
		FROM entity_shares s
		JOIN users u ON u.id = s.grantee_id
		LEFT JOIN message_reads r
		       ON r.entity_id = s.entity_id AND r.user_id = s.grantee_id
		WHERE s.entity_id = $1::uuid
		  AND s.mode = 'direct'
		  AND s.revoked_at IS NULL
		  AND (s.expires_at IS NULL OR s.expires_at > now())
		  AND EXISTS (
		      SELECT 1 FROM entities e
		      WHERE e.id = $1::uuid AND e.owner_id = $2::uuid
		  )
		ORDER BY s.granted_at, u.email`, messageID, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := make([]domain.MessageRecipient, 0)
	for rows.Next() {
		var r domain.MessageRecipient
		if err := rows.Scan(&r.UserID, &r.Email, &r.DisplayName, &r.ReadAt); err != nil {
			return nil, err
		}
		list = append(list, r)
	}
	return list, rows.Err()
}

// MarkMessageRead фиксирует первое прочтение сообщения получателем.
func (s *Storage) MarkMessageRead(ctx context.Context, userID, messageID string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO message_reads (entity_id, user_id)
		SELECT $1::uuid, $2::uuid
		WHERE EXISTS (
			SELECT 1 FROM entity_shares s
			WHERE s.entity_id = $1::uuid
			  AND s.grantee_id = $2::uuid
			  AND s.mode = 'direct'
			  AND s.revoked_at IS NULL
			  AND (s.expires_at IS NULL OR s.expires_at > now())
		)
		ON CONFLICT (entity_id, user_id) DO NOTHING`, messageID, userID)
	return err
}

// AddMessageRecipients добавляет новых получателей и уведомления для них.
func (s *Storage) AddMessageRecipients(ctx context.Context, ownerID, messageID string,
	recipients []domain.MessageRecipient) error {

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var text string
	if err := tx.QueryRow(ctx, `
		SELECT m.text
		FROM messages m
		JOIN entities e ON e.id = m.entity_id
		WHERE m.entity_id = $1::uuid AND e.owner_id = $2::uuid AND e.is_archived = FALSE`,
		messageID, ownerID).Scan(&text); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return err
	}

	payload := messagePayload(text)
	for _, r := range recipients {
		var shareID string
		err := tx.QueryRow(ctx, `
			INSERT INTO entity_shares (entity_id, grantor_id, grantee_id, mode, permission)
			VALUES ($1::uuid, $2::uuid, $3::uuid, 'direct', 'read')
			ON CONFLICT (entity_id, grantee_id) WHERE mode = 'direct' AND revoked_at IS NULL
			DO NOTHING
			RETURNING id::text`, messageID, ownerID, r.UserID).Scan(&shareID)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, `
			INSERT INTO notifications (user_id, kind, entity_id, share_id, actor_id, payload)
			VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5::uuid, $6::jsonb)`,
			r.UserID, domain.NotificationShareReceived, messageID, shareID, ownerID, payload); err != nil {
			return err
		}
	}

	return tx.Commit(ctx)
}

// RevokeMessageShare отзывает доступ получателя и создаёт уведомление.
func (s *Storage) RevokeMessageShare(ctx context.Context, ownerID, messageID, granteeID string) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var shareID string
	err = tx.QueryRow(ctx, `
		UPDATE entity_shares s
		SET revoked_at = now()
		WHERE s.entity_id = $1::uuid
		  AND s.grantee_id = $2::uuid
		  AND s.mode = 'direct'
		  AND s.revoked_at IS NULL
		  AND EXISTS (
		      SELECT 1 FROM entities e
		      WHERE e.id = $1::uuid AND e.owner_id = $3::uuid
		  )
		RETURNING s.id::text`, messageID, granteeID, ownerID).Scan(&shareID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.ErrNotFound
		}
		return err
	}

	if _, err := tx.Exec(ctx, `
		INSERT INTO notifications (user_id, kind, entity_id, share_id, actor_id, payload)
		VALUES ($1::uuid, $2, $3::uuid, $4::uuid, $5::uuid, '{}'::jsonb)`,
		granteeID, domain.NotificationShareRevoked, messageID, shareID, ownerID); err != nil {
		return err
	}

	return tx.Commit(ctx)
}

func messagePayload(text string) []byte {
	preview := strings.TrimSpace(text)
	if runes := []rune(preview); len(runes) > 120 {
		preview = string(runes[:120])
	}
	payload, _ := json.Marshal(map[string]any{
		"entity_type":  domain.EntityTypeMessage,
		"text_preview": preview,
	})
	return payload
}
