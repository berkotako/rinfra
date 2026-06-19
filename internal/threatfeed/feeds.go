package threatfeed

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// Feed is an operator-managed advisory source persisted by RInfra (as opposed to
// the static, env-configured base sources). A feed is either a remote URL or an
// inline JSON document, both in RInfra's native Advisory schema ("our data
// style"). Feeds are added/removed from the Settings screen and collected on
// every refresh alongside the base sources.
type Feed struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Kind      string    `json:"kind"` // "url" | "inline"
	URL       string    `json:"url,omitempty"`
	Inline    string    `json:"inline,omitempty"`
	Enabled   bool      `json:"enabled"`
	CreatedAt time.Time `json:"createdAt"`
	CreatedBy string    `json:"createdBy,omitempty"`
}

// Feed kinds.
const (
	FeedKindURL    = "url"
	FeedKindInline = "inline"
)

// FeedStore persists operator-managed advisory feeds.
type FeedStore interface {
	ListFeeds(ctx context.Context) ([]Feed, error)
	CreateFeed(ctx context.Context, f Feed) (Feed, error)
	DeleteFeed(ctx context.Context, id string) error
}

// ErrFeedsUnsupported is returned by feed management methods when the service
// was built without a FeedStore (e.g. a deployment with no database).
var ErrFeedsUnsupported = errors.New("threatfeed: persistent feeds are not configured")

// ErrInvalidFeed indicates a feed failed validation.
var ErrInvalidFeed = errors.New("threatfeed: invalid feed")

// Validate checks a feed's shape: a name, a known kind, and a usable payload.
// For inline feeds the JSON is parsed to guarantee it is collectible.
func (f Feed) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidFeed)
	}
	switch f.Kind {
	case FeedKindURL:
		u, err := url.Parse(strings.TrimSpace(f.URL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return fmt.Errorf("%w: url must be a valid http(s) URL", ErrInvalidFeed)
		}
	case FeedKindInline:
		if strings.TrimSpace(f.Inline) == "" {
			return fmt.Errorf("%w: inline JSON is required", ErrInvalidFeed)
		}
		if _, err := ParseAdvisories([]byte(f.Inline)); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidFeed, err)
		}
	default:
		return fmt.Errorf("%w: kind must be %q or %q", ErrInvalidFeed, FeedKindURL, FeedKindInline)
	}
	return nil
}

// feedToSource adapts a persisted feed to a Source.
func feedToSource(f Feed) Source {
	if f.Kind == FeedKindInline {
		return inlineSource{name: f.Name, data: []byte(f.Inline)}
	}
	return &JSONSource{SourceName: f.Name, URL: f.URL}
}

// inlineSource serves advisories from an in-memory JSON document.
type inlineSource struct {
	name string
	data []byte
}

func (s inlineSource) Name() string { return s.name }

func (s inlineSource) Fetch(_ context.Context) ([]Advisory, error) {
	adv, err := ParseAdvisories(s.data)
	if err != nil {
		return nil, err
	}
	for i := range adv {
		if adv[i].Source == "" {
			adv[i].Source = s.name
		}
	}
	return adv, nil
}

// ListFeeds returns the persisted feeds, or ErrFeedsUnsupported if no store.
func (s *Service) ListFeeds(ctx context.Context) ([]Feed, error) {
	if s.store == nil {
		return nil, ErrFeedsUnsupported
	}
	return s.store.ListFeeds(ctx)
}

// AddFeed validates and persists a new feed, then invalidates the cache so the
// next List collects it. The caller supplies Name/Kind/URL/Inline and createdBy.
func (s *Service) AddFeed(ctx context.Context, f Feed) (Feed, error) {
	if s.store == nil {
		return Feed{}, ErrFeedsUnsupported
	}
	if err := f.Validate(); err != nil {
		return Feed{}, err
	}
	f.ID = newFeedID()
	f.Enabled = true
	f.CreatedAt = time.Now().UTC()
	created, err := s.store.CreateFeed(ctx, f)
	if err != nil {
		return Feed{}, err
	}
	s.invalidate()
	return created, nil
}

// DeleteFeed removes a feed and invalidates the cache.
func (s *Service) DeleteFeed(ctx context.Context, id string) error {
	if s.store == nil {
		return ErrFeedsUnsupported
	}
	if err := s.store.DeleteFeed(ctx, id); err != nil {
		return err
	}
	s.invalidate()
	return nil
}

func (s *Service) invalidate() {
	s.mu.Lock()
	s.fetchedAt = time.Time{}
	s.mu.Unlock()
}

func newFeedID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return "feed_" + hex.EncodeToString(b)
}
