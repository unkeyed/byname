package notify

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/unkeyed/byname/internal/core"
)

// Channel is a notification channel as Settings shows it: its settings in
// the clear and never its secrets.
type Channel struct {
	ID             int64           `json:"id"`
	Name           string          `json:"name"`
	Kind           ChannelKind     `json:"kind"`
	Settings       json.RawMessage `json:"settings"`
	Events         []Kind          `json:"events"`
	Enabled        bool            `json:"enabled"`
	LastTestAt     *time.Time      `json:"last_test_at"`
	LastTestStatus string          `json:"last_test_status"`
	CreatedAt      time.Time       `json:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"`
}

// ChannelInput creates or replaces a channel. On an edit, Kind must stay
// the same and Secrets may name only the secrets to change.
type ChannelInput struct {
	Name     string          `json:"name"`
	Kind     ChannelKind     `json:"kind"`
	Settings json.RawMessage `json:"settings"`
	Secrets  json.RawMessage `json:"secrets,omitempty"`
	Events   []Kind          `json:"events"`
	Enabled  *bool           `json:"enabled,omitempty"`
}

// CreatedChannel is the one response that carries a webhook's signing
// secret.
type CreatedChannel struct {
	Channel
	SigningSecret string `json:"signing_secret,omitempty"`
}

// ChannelList is every channel and every event kind one can subscribe to.
type ChannelList struct {
	Channels []Channel  `json:"channels"`
	Events   []KindInfo `json:"events"`
}

const channelColumns = `id, name, kind, settings, events, enabled, last_test_at, coalesce(last_test_status, ''), created_at, updated_at`

func scanChannel(r pgx.Row) (Channel, error) {
	var c Channel
	return c, r.Scan(&c.ID, &c.Name, &c.Kind, &c.Settings, &c.Events, &c.Enabled, &c.LastTestAt, &c.LastTestStatus, &c.CreatedAt, &c.UpdatedAt)
}

func sealContext(k ChannelKind) string { return "notification_channel:" + string(k) }

func (s *Service) Channels(ctx context.Context) (ChannelList, error) {
	rows, err := s.db.Query(ctx, `SELECT `+channelColumns+` FROM notification_channel ORDER BY id`)
	if err != nil {
		return ChannelList{}, err
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Channel, error) { return scanChannel(r) })
	return ChannelList{Channels: list, Events: Kinds()}, err
}

func (s *Service) channel(ctx context.Context, id int64) (Channel, error) {
	c, err := scanChannel(s.db.QueryRow(ctx, `SELECT `+channelColumns+` FROM notification_channel WHERE id = $1`, id))
	if err == pgx.ErrNoRows {
		return c, fmt.Errorf("%w: no notification channel %d", core.ErrNotFound, id)
	}
	return c, err
}

// openChannel reads a channel and unseals its transport.
func (s *Service) openChannel(ctx context.Context, id int64) (Channel, transport, error) {
	var c Channel
	var sealed string
	err := s.db.QueryRow(ctx, `SELECT `+channelColumns+`, ciphertext FROM notification_channel WHERE id = $1`, id).
		Scan(&c.ID, &c.Name, &c.Kind, &c.Settings, &c.Events, &c.Enabled, &c.LastTestAt, &c.LastTestStatus, &c.CreatedAt, &c.UpdatedAt, &sealed)
	if err == pgx.ErrNoRows {
		return c, nil, fmt.Errorf("%w: no notification channel %d", core.ErrNotFound, id)
	}
	if err != nil {
		return c, nil, err
	}
	secrets, err := s.secrets.Open(sealed, sealContext(c.Kind))
	if err != nil {
		return c, nil, fmt.Errorf("open channel %d's secrets: %w; was ENCRYPTION_KEY changed without ENCRYPTION_KEY_PREVIOUS?", id, err)
	}
	t, err := channelKinds[c.Kind].parse(c.Settings, json.RawMessage(secrets))
	return c, t, err
}

func (in *ChannelInput) clean() error {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	if in.Name == "" || len([]rune(in.Name)) > 80 {
		return fmt.Errorf("%w: name must be 1 to 80 characters", core.ErrInvalid)
	}
	events := []Kind{}
	for _, k := range in.Events {
		if _, ok := lookup(k); !ok {
			return fmt.Errorf("%w: %q is not an event kind; GET /v1/notification-channels lists them", core.ErrInvalid, k)
		}
		if !slices.Contains(events, k) {
			events = append(events, k)
		}
	}
	in.Events = events
	return nil
}

// sealParts stores a transport's settings in the clear and seals its
// secrets.
func (s *Service) sealParts(k ChannelKind, t transport) (settings []byte, sealed string, err error) {
	set, sec := t.parts()
	if settings, err = json.Marshal(set); err != nil {
		return nil, "", err
	}
	secrets, err := json.Marshal(sec)
	if err != nil {
		return nil, "", err
	}
	return settings, s.secrets.Seal(string(secrets), sealContext(k)), nil
}

// CreateChannel adds a channel. A webhook's signing secret is made here
// and returned this once.
func (s *Service) CreateChannel(ctx context.Context, in ChannelInput) (CreatedChannel, error) {
	if err := in.clean(); err != nil {
		return CreatedChannel{}, err
	}
	kind, ok := channelKinds[in.Kind]
	if !ok {
		return CreatedChannel{}, fmt.Errorf("%w: kind must be smtp, email_api, slack_webhook or webhook", core.ErrInvalid)
	}
	if kind.generated != nil && len(in.Secrets) > 0 {
		return CreatedChannel{}, fmt.Errorf("%w: a %s channel's secret is made by Byname; leave secrets out", core.ErrInvalid, in.Kind)
	}
	t, err := kind.parse(in.Settings, in.Secrets)
	if err != nil {
		return CreatedChannel{}, err
	}
	var out CreatedChannel
	if kind.generated != nil {
		out.SigningSecret = kind.generated(t)
	}
	settings, sealed, err := s.sealParts(in.Kind, t)
	if err != nil {
		return out, err
	}
	enabled := in.Enabled == nil || *in.Enabled
	out.Channel, err = scanChannel(s.db.QueryRow(ctx, `INSERT INTO notification_channel (name, kind, settings, ciphertext, events, enabled)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING `+channelColumns, in.Name, in.Kind, settings, sealed, in.Events, enabled))
	return out, nameTaken(err, in.Name)
}

// UpdateChannel replaces a channel's name, settings, events and enabled
// state, and the secrets in.Secrets names. Secrets it leaves out stay.
func (s *Service) UpdateChannel(ctx context.Context, id int64, in ChannelInput) (Channel, error) {
	if err := in.clean(); err != nil {
		return Channel{}, err
	}
	cur, t, err := s.openChannel(ctx, id)
	if err != nil {
		return Channel{}, err
	}
	if in.Kind != "" && in.Kind != cur.Kind {
		return Channel{}, fmt.Errorf("%w: a channel's kind cannot change; add a new %s channel instead", core.ErrInvalid, in.Kind)
	}
	kind := channelKinds[cur.Kind]
	if kind.generated != nil && len(in.Secrets) > 0 {
		return Channel{}, fmt.Errorf("%w: a %s channel's signing secret cannot be changed; add a new channel for a new secret", core.ErrInvalid, cur.Kind)
	}
	_, stored := t.parts()
	storedJSON, err := json.Marshal(stored)
	if err != nil {
		return Channel{}, err
	}
	if t, err = kind.parse(in.Settings, storedJSON, in.Secrets); err != nil {
		return Channel{}, err
	}
	settings, sealed, err := s.sealParts(cur.Kind, t)
	if err != nil {
		return Channel{}, err
	}
	enabled := cur.Enabled
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	c, err := scanChannel(s.db.QueryRow(ctx, `UPDATE notification_channel SET name = $2, settings = $3, ciphertext = $4, events = $5,
			enabled = $6, updated_at = $7
		WHERE id = $1 RETURNING `+channelColumns, id, in.Name, settings, sealed, in.Events, enabled, s.now()))
	return c, nameTaken(err, in.Name)
}

func nameTaken(err error, name string) error {
	var pg *pgconn.PgError
	if err != nil && errors.As(err, &pg) && pg.Code == "23505" && pg.ConstraintName == "notification_channel_name" {
		return fmt.Errorf("%w: the name %q is already used for notifications going somewhere else", core.ErrConflict, name)
	}
	return err
}

// DeleteChannel removes a channel and its delivery log.
func (s *Service) DeleteChannel(ctx context.Context, id int64) error {
	tag, err := s.db.Exec(ctx, `DELETE FROM notification_channel WHERE id = $1`, id)
	if err == nil && tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: no notification channel %d", core.ErrNotFound, id)
	}
	return err
}

// TestChannel sends a test message now and records how it went on the
// channel. A failed test is a result, not an error.
func (s *Service) TestChannel(ctx context.Context, id int64) (Channel, error) {
	c, t, err := s.openChannel(ctx, id)
	if err != nil {
		return Channel{}, err
	}
	now := s.now()
	e, err := s.envelope(ctx, fmt.Sprintf("test-%d/channel-%d", now.UnixNano(), id), testPayload{}, c.Name, now)
	if err != nil {
		return Channel{}, err
	}
	e.Payload = json.RawMessage(`{}`)
	status := "ok"
	if err := t.send(ctx, s, e); err != nil {
		status = err.Error()
	}
	if _, err := s.db.Exec(ctx, `UPDATE notification_channel SET last_test_at = $2, last_test_status = $3 WHERE id = $1`, id, now, status); err != nil {
		return Channel{}, err
	}
	return s.channel(ctx, id)
}

// Delivery is one alert's delivery to one channel, as the delivery log
// shows it.
type Delivery struct {
	ID            int64       `json:"id"`
	AlertID       int64       `json:"alert_id"`
	Kind          Kind        `json:"kind"`
	Title         string      `json:"title"`
	ChannelID     int64       `json:"channel_id"`
	ChannelName   string      `json:"channel_name"`
	ChannelKind   ChannelKind `json:"channel_kind"`
	Status        string      `json:"status" jsonschema:"pending, sent or failed"`
	Attempts      int         `json:"attempts"`
	Error         string      `json:"error" jsonschema:"why the last attempt failed"`
	SentAt        *time.Time  `json:"sent_at"`
	NextAttemptAt *time.Time  `json:"next_attempt_at" jsonschema:"when a pending delivery is tried again"`
	CreatedAt     time.Time   `json:"created_at"`
}

type DeliveryQuery struct {
	Channel int64
	Limit   int
}

// Deliveries is the delivery log, newest first.
func (s *Service) Deliveries(ctx context.Context, q DeliveryQuery) ([]Delivery, error) {
	if q.Limit == 0 {
		q.Limit = 50
	}
	if q.Limit < 1 || q.Limit > 200 {
		return nil, fmt.Errorf("%w: limit must be 1 to 200", core.ErrInvalid)
	}
	rows, err := s.db.Query(ctx, `SELECT d.id, d.alert_id, a.kind, a.payload, d.channel_id, c.name, c.kind, d.status, d.attempts,
			coalesce(d.error, ''), d.sent_at, CASE WHEN d.status = 'pending' THEN d.next_attempt_at END, d.created_at
		FROM alert_delivery d JOIN alert a ON a.id = d.alert_id JOIN notification_channel c ON c.id = d.channel_id
		WHERE $1 = 0 OR d.channel_id = $1
		ORDER BY d.id DESC LIMIT $2`, q.Channel, q.Limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Delivery, error) {
		var d Delivery
		var payload json.RawMessage
		if err := r.Scan(&d.ID, &d.AlertID, &d.Kind, &payload, &d.ChannelID, &d.ChannelName, &d.ChannelKind, &d.Status, &d.Attempts,
			&d.Error, &d.SentAt, &d.NextAttemptAt, &d.CreatedAt); err != nil {
			return d, err
		}
		d.Title = string(d.Kind)
		if info, ok := lookup(d.Kind); ok {
			if p, err := info.decode(payload); err == nil {
				d.Title = p.message().Title
			}
		}
		return d, nil
	})
}

// SealChannels reseals channel secrets sealed under ENCRYPTION_KEY_PREVIOUS
// with ENCRYPTION_KEY, so a key rotation finishes with one restart.
func (s *Service) SealChannels(ctx context.Context) (int, error) {
	rows, err := s.db.Query(ctx, `SELECT id, kind, ciphertext FROM notification_channel`)
	if err != nil {
		return 0, err
	}
	type row struct {
		id     int64
		kind   ChannelKind
		sealed string
	}
	list, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (row, error) {
		var x row
		return x, r.Scan(&x.id, &x.kind, &x.sealed)
	})
	if err != nil {
		return 0, err
	}
	n := 0
	for _, x := range list {
		resealed, changed, err := s.secrets.Reseal(x.sealed, sealContext(x.kind))
		if err != nil {
			return n, err
		}
		if !changed {
			continue
		}
		if _, err := s.db.Exec(ctx, `UPDATE notification_channel SET ciphertext = $2 WHERE id = $1 AND ciphertext = $3`, x.id, resealed, x.sealed); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
