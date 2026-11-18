package persistence

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/change"
	"github.com/aognio/dns-change-monitor/pkg/domain/pairing"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
	"github.com/aognio/dns-change-monitor/pkg/domain/subscriber"
	"gorm.io/gorm"
)

// Repositories bundles every repository backed by the same database.
type Repositories struct {
	Targets     *TargetRepository
	Snapshots   *SnapshotRepository
	Changes     *ChangeRepository
	Subscribers *SubscriberRepository
	Tokens      *TokenRepository
}

// NewRepositories builds the repositories on top of an open database.
func NewRepositories(db *gorm.DB) Repositories {
	return Repositories{
		Targets:     &TargetRepository{db: db},
		Snapshots:   &SnapshotRepository{db: db},
		Changes:     &ChangeRepository{db: db},
		Subscribers: &SubscriberRepository{db: db},
		Tokens:      &TokenRepository{db: db},
	}
}

// TargetRepository stores monitored DNS names.
type TargetRepository struct {
	db *gorm.DB
}

// List implements record.TargetRepository.
func (r *TargetRepository) List(ctx context.Context) ([]record.Target, error) {
	var models []TargetModel
	if err := r.db.WithContext(ctx).Order("name asc, record_type asc").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list targets: %w", err)
	}
	targets := make([]record.Target, 0, len(models))
	for _, model := range models {
		targets = append(targets, targetFromModel(model))
	}
	return targets, nil
}

// Get implements record.TargetRepository.
func (r *TargetRepository) Get(ctx context.Context, id string) (record.Target, error) {
	var model TargetModel
	if err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return record.Target{}, fmt.Errorf("%w: target %s", shared.ErrNotFound, id)
		}
		return record.Target{}, fmt.Errorf("get target: %w", err)
	}
	return targetFromModel(model), nil
}

// Upsert implements record.TargetRepository. A target keeping the same name,
// type and resolver is updated in place so that its identifier stays stable.
func (r *TargetRepository) Upsert(ctx context.Context, target record.Target) error {
	var model TargetModel
	err := r.db.WithContext(ctx).
		First(&model, "name = ? AND record_type = ? AND resolver = ?", target.Name, string(target.Type), target.Resolver).
		Error
	switch {
	case err == nil:
		model.Enabled = target.Enabled
		model.Source = target.Source
		model.CheckMode = string(target.Check.EffectiveMode())
		model.CheckExpect = StringList(target.Check.Expect)
		model.UpdatedAt = target.UpdatedAt
		if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
			return fmt.Errorf("update target: %w", err)
		}
		return nil
	case errors.Is(err, gorm.ErrRecordNotFound):
		model = TargetModel{
			ID:          target.ID,
			Name:        target.Name,
			RecordType:  string(target.Type),
			Resolver:    target.Resolver,
			Enabled:     target.Enabled,
			Source:      target.Source,
			CheckMode:   string(target.Check.EffectiveMode()),
			CheckExpect: StringList(target.Check.Expect),
			CreatedAt:   target.CreatedAt,
			UpdatedAt:   target.UpdatedAt,
		}
		if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
			return fmt.Errorf("create target: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("look up target: %w", err)
	}
}

// Delete implements record.TargetRepository.
func (r *TargetRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&TargetModel{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("delete target: %w", err)
	}
	return nil
}

func targetFromModel(model TargetModel) record.Target {
	mode := record.Mode(model.CheckMode)
	if mode == "" {
		mode = record.ModeChange
	}
	return record.Target{
		ID:        model.ID,
		Name:      model.Name,
		Type:      record.Type(model.RecordType),
		Resolver:  model.Resolver,
		Enabled:   model.Enabled,
		Source:    model.Source,
		CreatedAt: model.CreatedAt,
		UpdatedAt: model.UpdatedAt,
		Check: record.Check{
			Mode:   mode,
			Expect: []string(model.CheckExpect),
		},
	}
}

// SnapshotRepository stores resolution results.
type SnapshotRepository struct {
	db *gorm.DB
}

// Latest implements record.SnapshotRepository.
func (r *SnapshotRepository) Latest(ctx context.Context, targetID string) (record.Snapshot, error) {
	var model SnapshotModel
	err := r.db.WithContext(ctx).
		Where("target_id = ?", targetID).
		Order("observed_at desc").
		First(&model).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return record.Snapshot{}, fmt.Errorf("%w: snapshot for target %s", shared.ErrNotFound, targetID)
		}
		return record.Snapshot{}, fmt.Errorf("load snapshot: %w", err)
	}
	return record.Snapshot{
		ID:         model.ID,
		TargetID:   model.TargetID,
		Values:     []string(model.Values),
		TTL:        time.Duration(model.TTLSeconds) * time.Second,
		ObservedAt: model.ObservedAt,
		Satisfied:  model.Satisfied,
		Reason:     model.Reason,
	}, nil
}

// Save implements record.SnapshotRepository.
func (r *SnapshotRepository) Save(ctx context.Context, snapshot record.Snapshot) error {
	model := SnapshotModel{
		ID:         snapshot.ID,
		TargetID:   snapshot.TargetID,
		Values:     StringList(snapshot.Values),
		TTLSeconds: int64(snapshot.TTL / time.Second),
		ObservedAt: snapshot.ObservedAt,
		Satisfied:  snapshot.Satisfied,
		Reason:     snapshot.Reason,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return fmt.Errorf("save snapshot: %w", err)
	}
	return nil
}

// ChangeRepository stores detected differences.
type ChangeRepository struct {
	db *gorm.DB
}

// Save implements change.Repository.
func (r *ChangeRepository) Save(ctx context.Context, detected change.Change) error {
	model := ChangeModel{
		ID:           detected.ID,
		TargetID:     detected.TargetID,
		TargetName:   detected.TargetName,
		RecordType:   string(detected.RecordType),
		Added:        StringList(detected.Added),
		Removed:      StringList(detected.Removed),
		DetectedAt:   detected.DetectedAt,
		NotifiedAt:   detected.NotifiedAt,
		Kind:         string(changeKind(detected.Kind)),
		Mode:         string(detected.Mode),
		AssertReason: detected.Reason,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return fmt.Errorf("save change: %w", err)
	}
	return nil
}

// MarkNotified implements change.Repository.
func (r *ChangeRepository) MarkNotified(ctx context.Context, id string, at time.Time) error {
	result := r.db.WithContext(ctx).Model(&ChangeModel{}).Where("id = ?", id).Update("notified_at", at)
	if result.Error != nil {
		return fmt.Errorf("mark change notified: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: change %s", shared.ErrNotFound, id)
	}
	return nil
}

// ListPending implements change.Repository.
func (r *ChangeRepository) ListPending(ctx context.Context) ([]change.Change, error) {
	var models []ChangeModel
	if err := r.db.WithContext(ctx).
		Where("notified_at IS NULL").
		Order("detected_at asc").
		Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list pending changes: %w", err)
	}
	return changesFromModels(models), nil
}

// List implements change.Repository.
func (r *ChangeRepository) List(ctx context.Context, limit int) ([]change.Change, error) {
	query := r.db.WithContext(ctx).Order("detected_at desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var models []ChangeModel
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list changes: %w", err)
	}
	return changesFromModels(models), nil
}

func changesFromModels(models []ChangeModel) []change.Change {
	changes := make([]change.Change, 0, len(models))
	for _, model := range models {
		changes = append(changes, change.Change{
			ID:         model.ID,
			TargetID:   model.TargetID,
			TargetName: model.TargetName,
			RecordType: record.Type(model.RecordType),
			Added:      []string(model.Added),
			Removed:    []string(model.Removed),
			DetectedAt: model.DetectedAt,
			NotifiedAt: model.NotifiedAt,
			Kind:       changeKind(change.Kind(model.Kind)),
			Mode:       record.Mode(model.Mode),
			Reason:     model.AssertReason,
		})
	}
	return changes
}

// changeKind normalises the stored kind, so that the rows created before the
// check modes existed are read as value changes.
func changeKind(kind change.Kind) change.Kind {
	switch kind {
	case change.KindViolated:
		return change.KindViolated
	case change.KindRecovered:
		return change.KindRecovered
	default:
		return change.KindValue
	}
}

// SubscriberRepository stores notifyable Telegram accounts.
type SubscriberRepository struct {
	db *gorm.DB
}

// List implements subscriber.Repository.
func (r *SubscriberRepository) List(ctx context.Context) ([]subscriber.Subscriber, error) {
	var models []SubscriberModel
	if err := r.db.WithContext(ctx).Order("username asc").Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list subscribers: %w", err)
	}
	result := make([]subscriber.Subscriber, 0, len(models))
	for _, model := range models {
		result = append(result, subscriberFromModel(model))
	}
	return result, nil
}

// Get implements subscriber.Repository.
func (r *SubscriberRepository) Get(ctx context.Context, id string) (subscriber.Subscriber, error) {
	var model SubscriberModel
	if err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return subscriber.Subscriber{}, fmt.Errorf("%w: subscriber %s", shared.ErrNotFound, id)
		}
		return subscriber.Subscriber{}, fmt.Errorf("get subscriber: %w", err)
	}
	return subscriberFromModel(model), nil
}

// FindByUsername implements subscriber.Repository.
func (r *SubscriberRepository) FindByUsername(ctx context.Context, username string) (subscriber.Subscriber, error) {
	var model SubscriberModel
	if err := r.db.WithContext(ctx).First(&model, "username = ?", username).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return subscriber.Subscriber{}, fmt.Errorf("%w: subscriber %s", shared.ErrNotFound, username)
		}
		return subscriber.Subscriber{}, fmt.Errorf("find subscriber: %w", err)
	}
	return subscriberFromModel(model), nil
}

// FindByTelegramUserID implements subscriber.Repository.
func (r *SubscriberRepository) FindByTelegramUserID(ctx context.Context, telegramUserID int64) (subscriber.Subscriber, error) {
	var model SubscriberModel
	if err := r.db.WithContext(ctx).First(&model, "telegram_user_id = ?", telegramUserID).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return subscriber.Subscriber{}, fmt.Errorf("%w: telegram user %d", shared.ErrNotFound, telegramUserID)
		}
		return subscriber.Subscriber{}, fmt.Errorf("find subscriber: %w", err)
	}
	return subscriberFromModel(model), nil
}

// Upsert implements subscriber.Repository.
func (r *SubscriberRepository) Upsert(ctx context.Context, entry subscriber.Subscriber) error {
	model := SubscriberModel{
		ID:             entry.ID,
		Username:       entry.Username,
		TelegramUserID: entry.TelegramUserID,
		ChatID:         entry.ChatID,
		Status:         string(entry.Status),
		CreatedAt:      entry.CreatedAt,
		UpdatedAt:      entry.UpdatedAt,
		PairedAt:       entry.PairedAt,
	}
	if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
		return fmt.Errorf("save subscriber: %w", err)
	}
	return nil
}

// Delete implements subscriber.Repository.
func (r *SubscriberRepository) Delete(ctx context.Context, id string) error {
	if err := r.db.WithContext(ctx).Delete(&SubscriberModel{}, "id = ?", id).Error; err != nil {
		return fmt.Errorf("delete subscriber: %w", err)
	}
	return nil
}

func subscriberFromModel(model SubscriberModel) subscriber.Subscriber {
	return subscriber.Subscriber{
		ID:             model.ID,
		Username:       model.Username,
		TelegramUserID: model.TelegramUserID,
		ChatID:         model.ChatID,
		Status:         subscriber.Status(model.Status),
		CreatedAt:      model.CreatedAt,
		UpdatedAt:      model.UpdatedAt,
		PairedAt:       model.PairedAt,
	}
}

// TokenRepository stores pairing tokens.
type TokenRepository struct {
	db *gorm.DB
}

// Create implements pairing.Repository.
func (r *TokenRepository) Create(ctx context.Context, token pairing.Token) error {
	model := PairingTokenModel{
		ID:         token.ID,
		CreatedAt:  token.CreatedAt,
		ExpiresAt:  token.ExpiresAt,
		ConsumedAt: token.ConsumedAt,
		ConsumedBy: token.ConsumedBy,
		CreatedBy:  token.CreatedBy,
	}
	if err := r.db.WithContext(ctx).Create(&model).Error; err != nil {
		return fmt.Errorf("create pairing token: %w", err)
	}
	return nil
}

// Get implements pairing.Repository.
func (r *TokenRepository) Get(ctx context.Context, id string) (pairing.Token, error) {
	var model PairingTokenModel
	if err := r.db.WithContext(ctx).First(&model, "id = ?", id).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return pairing.Token{}, fmt.Errorf("%w: pairing token %s", shared.ErrNotFound, id)
		}
		return pairing.Token{}, fmt.Errorf("get pairing token: %w", err)
	}
	return tokenFromModel(model), nil
}

// Save implements pairing.Repository.
func (r *TokenRepository) Save(ctx context.Context, token pairing.Token) error {
	model := PairingTokenModel{
		ID:         token.ID,
		CreatedAt:  token.CreatedAt,
		ExpiresAt:  token.ExpiresAt,
		ConsumedAt: token.ConsumedAt,
		ConsumedBy: token.ConsumedBy,
		CreatedBy:  token.CreatedBy,
	}
	if err := r.db.WithContext(ctx).Save(&model).Error; err != nil {
		return fmt.Errorf("save pairing token: %w", err)
	}
	return nil
}

// List implements pairing.Repository.
func (r *TokenRepository) List(ctx context.Context, limit int) ([]pairing.Token, error) {
	query := r.db.WithContext(ctx).Order("created_at desc")
	if limit > 0 {
		query = query.Limit(limit)
	}
	var models []PairingTokenModel
	if err := query.Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list pairing tokens: %w", err)
	}
	tokens := make([]pairing.Token, 0, len(models))
	for _, model := range models {
		tokens = append(tokens, tokenFromModel(model))
	}
	return tokens, nil
}

func tokenFromModel(model PairingTokenModel) pairing.Token {
	return pairing.Token{
		ID:         model.ID,
		CreatedAt:  model.CreatedAt,
		ExpiresAt:  model.ExpiresAt,
		ConsumedAt: model.ConsumedAt,
		ConsumedBy: model.ConsumedBy,
		CreatedBy:  model.CreatedBy,
	}
}
