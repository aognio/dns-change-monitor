// Package testsupport holds the in-memory test doubles used by the test
// suites of the monitor.
package testsupport

import (
	"context"
	"sync"
	"time"

	"github.com/aognio/dns-change-monitor/pkg/domain/change"
	"github.com/aognio/dns-change-monitor/pkg/domain/notification"
	"github.com/aognio/dns-change-monitor/pkg/domain/pairing"
	"github.com/aognio/dns-change-monitor/pkg/domain/record"
	"github.com/aognio/dns-change-monitor/pkg/domain/shared"
	"github.com/aognio/dns-change-monitor/pkg/domain/subscriber"
)

// Targets is an in-memory record.TargetRepository.
type Targets struct {
	mutex    sync.Mutex
	items    []record.Target
	nextID   int
	Upserted []record.Target
}

// NewTargets builds an empty target repository.
func NewTargets(targets ...record.Target) *Targets {
	return &Targets{items: targets, nextID: len(targets)}
}

// List implements record.TargetRepository.
func (t *Targets) List(context.Context) ([]record.Target, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return append([]record.Target(nil), t.items...), nil
}

// Get implements record.TargetRepository.
func (t *Targets) Get(_ context.Context, id string) (record.Target, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for _, target := range t.items {
		if target.ID == id {
			return target, nil
		}
	}
	return record.Target{}, shared.ErrNotFound
}

// Upsert implements record.TargetRepository.
func (t *Targets) Upsert(_ context.Context, target record.Target) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.Upserted = append(t.Upserted, target)
	for index, existing := range t.items {
		if existing.Identity() == target.Identity() {
			t.items[index] = target
			return nil
		}
	}
	t.items = append(t.items, target)
	return nil
}

// Delete implements record.TargetRepository.
func (t *Targets) Delete(_ context.Context, id string) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	for index, existing := range t.items {
		if existing.ID == id {
			t.items = append(t.items[:index], t.items[index+1:]...)
			return nil
		}
	}
	return nil
}

// Snapshots is an in-memory record.SnapshotRepository.
type Snapshots struct {
	mutex sync.Mutex
	items []record.Snapshot
}

// NewSnapshots builds an empty snapshot repository.
func NewSnapshots(snapshots ...record.Snapshot) *Snapshots {
	return &Snapshots{items: snapshots}
}

// Latest implements record.SnapshotRepository.
func (s *Snapshots) Latest(_ context.Context, targetID string) (record.Snapshot, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	var latest record.Snapshot
	found := false
	for _, snapshot := range s.items {
		if snapshot.TargetID != targetID {
			continue
		}
		if !found || snapshot.ObservedAt.After(latest.ObservedAt) {
			latest = snapshot
			found = true
		}
	}
	if !found {
		return record.Snapshot{}, shared.ErrNotFound
	}
	return latest, nil
}

// Save implements record.SnapshotRepository.
func (s *Snapshots) Save(_ context.Context, snapshot record.Snapshot) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.items = append(s.items, snapshot)
	return nil
}

// Changes is an in-memory change.Repository.
type Changes struct {
	mutex    sync.Mutex
	items    []change.Change
	Notified []string
}

// NewChanges builds an empty change repository.
func NewChanges() *Changes { return &Changes{} }

// Save implements change.Repository.
func (c *Changes) Save(_ context.Context, detected change.Change) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.items = append(c.items, detected)
	return nil
}

// MarkNotified implements change.Repository.
func (c *Changes) MarkNotified(_ context.Context, id string, at time.Time) error {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	c.Notified = append(c.Notified, id)
	for index := range c.items {
		if c.items[index].ID == id {
			c.items[index].NotifiedAt = &at
		}
	}
	return nil
}

// ListPending implements change.Repository.
func (c *Changes) ListPending(context.Context) ([]change.Change, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	pending := make([]change.Change, 0, len(c.items))
	for _, detected := range c.items {
		if !detected.IsNotified() {
			pending = append(pending, detected)
		}
	}
	return pending, nil
}

// List implements change.Repository.
func (c *Changes) List(context.Context, int) ([]change.Change, error) {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return append([]change.Change(nil), c.items...), nil
}

// Items returns the stored changes.
func (c *Changes) Items() []change.Change {
	c.mutex.Lock()
	defer c.mutex.Unlock()
	return append([]change.Change(nil), c.items...)
}

// Subscribers is an in-memory subscriber.Repository.
type Subscribers struct {
	mutex sync.Mutex
	items []subscriber.Subscriber
}

// NewSubscribers builds an empty subscriber repository.
func NewSubscribers(subscribers ...subscriber.Subscriber) *Subscribers {
	return &Subscribers{items: subscribers}
}

// List implements subscriber.Repository.
func (s *Subscribers) List(context.Context) ([]subscriber.Subscriber, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	return append([]subscriber.Subscriber(nil), s.items...), nil
}

// Get implements subscriber.Repository.
func (s *Subscribers) Get(_ context.Context, id string) (subscriber.Subscriber, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, entry := range s.items {
		if entry.ID == id {
			return entry, nil
		}
	}
	return subscriber.Subscriber{}, shared.ErrNotFound
}

// FindByUsername implements subscriber.Repository.
func (s *Subscribers) FindByUsername(_ context.Context, username string) (subscriber.Subscriber, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, entry := range s.items {
		if entry.Username == username {
			return entry, nil
		}
	}
	return subscriber.Subscriber{}, shared.ErrNotFound
}

// FindByTelegramUserID implements subscriber.Repository.
func (s *Subscribers) FindByTelegramUserID(_ context.Context, telegramUserID int64) (subscriber.Subscriber, error) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for _, entry := range s.items {
		if entry.TelegramUserID == telegramUserID {
			return entry, nil
		}
	}
	return subscriber.Subscriber{}, shared.ErrNotFound
}

// Upsert implements subscriber.Repository.
func (s *Subscribers) Upsert(_ context.Context, entry subscriber.Subscriber) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for index, existing := range s.items {
		if existing.ID == entry.ID {
			s.items[index] = entry
			return nil
		}
	}
	s.items = append(s.items, entry)
	return nil
}

// Delete implements subscriber.Repository.
func (s *Subscribers) Delete(_ context.Context, id string) error {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	for index, existing := range s.items {
		if existing.ID == id {
			s.items = append(s.items[:index], s.items[index+1:]...)
			return nil
		}
	}
	return nil
}

// Tokens is an in-memory pairing.Repository.
type Tokens struct {
	mutex sync.Mutex
	items map[string]pairing.Token
}

// NewTokens builds an empty token repository.
func NewTokens() *Tokens { return &Tokens{items: make(map[string]pairing.Token)} }

// Create implements pairing.Repository.
func (t *Tokens) Create(_ context.Context, token pairing.Token) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.items[token.ID] = token
	return nil
}

// Get implements pairing.Repository.
func (t *Tokens) Get(_ context.Context, id string) (pairing.Token, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	token, ok := t.items[id]
	if !ok {
		return pairing.Token{}, shared.ErrNotFound
	}
	return token, nil
}

// Save implements pairing.Repository.
func (t *Tokens) Save(_ context.Context, token pairing.Token) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	t.items[token.ID] = token
	return nil
}

// List implements pairing.Repository.
func (t *Tokens) List(context.Context, int) ([]pairing.Token, error) {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	tokens := make([]pairing.Token, 0, len(t.items))
	for _, token := range t.items {
		tokens = append(tokens, token)
	}
	return tokens, nil
}

// Resolver answers with the values prepared by the test.
type Resolver struct {
	mutex   sync.Mutex
	answers map[string][]string
	failure map[string]error
}

// NewResolver builds a resolver returning the given answers.
func NewResolver() *Resolver {
	return &Resolver{
		answers: make(map[string][]string),
		failure: make(map[string]error),
	}
}

// SetAnswer prepares the answer of a target and clears its failure.
func (r *Resolver) SetAnswer(identity string, values ...string) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.answers[identity] = values
	delete(r.failure, identity)
}

// SetFailure prepares a failure for a target and clears its answer.
func (r *Resolver) SetFailure(identity string, err error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	r.failure[identity] = err
	delete(r.answers, identity)
}

// Resolve implements record.Resolver.
func (r *Resolver) Resolve(_ context.Context, target record.Target) (record.Resolution, error) {
	r.mutex.Lock()
	defer r.mutex.Unlock()
	if err, ok := r.failure[target.Identity()]; ok {
		return record.Resolution{}, err
	}
	values, ok := r.answers[target.Identity()]
	if !ok {
		return record.Resolution{}, shared.ErrNotFound
	}
	return record.Resolution{Values: values, TTL: time.Minute}, nil
}

// Notifier records the messages it is asked to deliver.
type Notifier struct {
	mutex    sync.Mutex
	Messages []notification.Message
	Targets  []notification.Recipient
	failure  error
}

// NewNotifier builds a recording notifier.
func NewNotifier() *Notifier { return &Notifier{} }

// Fail makes every delivery fail with the given error.
func (n *Notifier) Fail(err error) {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	n.failure = err
}

// Send implements notification.Notifier.
func (n *Notifier) Send(_ context.Context, recipient notification.Recipient, message notification.Message) error {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	if n.failure != nil {
		return n.failure
	}
	n.Targets = append(n.Targets, recipient)
	n.Messages = append(n.Messages, message)
	return nil
}

// Delivered returns the number of delivered messages.
func (n *Notifier) Delivered() int {
	n.mutex.Lock()
	defer n.mutex.Unlock()
	return len(n.Messages)
}

// Bus collects the published events.
type Bus struct {
	mutex  sync.Mutex
	Events []shared.Event
}

// NewBus builds an empty bus.
func NewBus() *Bus { return &Bus{} }

// Publish implements shared.EventBus.
func (b *Bus) Publish(_ context.Context, event shared.Event) error {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	b.Events = append(b.Events, event)
	return nil
}

// Subscribe implements shared.EventBus.
func (b *Bus) Subscribe(string, shared.EventHandler) {}

// Names returns the names of the published events.
func (b *Bus) Names() []string {
	b.mutex.Lock()
	defer b.mutex.Unlock()
	names := make([]string, 0, len(b.Events))
	for _, event := range b.Events {
		names = append(names, event.Name())
	}
	return names
}
