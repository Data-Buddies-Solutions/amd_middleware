package session

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"advancedmd-token-management/internal/domain"
)

// Session is the only interface through which callers obtain or maintain an
// AdvancedMD session. Returned token data is a copy and cannot mutate the
// implementation's last-known-good state.
type Session interface {
	Get(context.Context) (*domain.TokenData, error)
	Maintain(context.Context) error
	Status() SessionStatus
}

// SessionState is the externally observable lifecycle state.
type SessionState string

const (
	SessionUninitialized SessionState = "uninitialized"
	SessionRefreshing    SessionState = "refreshing"
	SessionFresh         SessionState = "fresh"
	SessionStale         SessionState = "stale"
	SessionDegraded      SessionState = "degraded"
	SessionUnavailable   SessionState = "unavailable"
)

const (
	// DefaultSessionExpiresAfter preserves the deployed 20-hour rotation
	// boundary as the local safety limit. It does not claim a longer,
	// undocumented provider token lifetime.
	DefaultSessionExpiresAfter = 20 * time.Hour
	// DefaultSessionStaleAfter starts request-time recovery one hour before the
	// existing rotation boundary.
	DefaultSessionStaleAfter = DefaultSessionExpiresAfter - time.Hour
	// DefaultSessionLoginTimeout bounds the complete two-step login flow.
	DefaultSessionLoginTimeout = 60 * time.Second
)

// ErrSessionUnavailable is returned when authentication failed and no
// last-known-good session remains inside the configured safe window.
var ErrSessionUnavailable = errors.New("advancedmd session unavailable")

// SessionStatus reports lifecycle state without exposing credentials, tokens,
// or provider endpoints.
type SessionStatus struct {
	State     SessionState
	TokenAge  time.Duration
	ExpiresIn time.Duration
}

type loginAdapter interface {
	Authenticate(context.Context) (token, webserverURL string, err error)
}

type sessionPolicy struct {
	staleAfter   time.Duration
	expiresAfter time.Duration
	loginTimeout time.Duration
}

type sessionImpl struct {
	login  loginAdapter
	now    func() time.Time
	policy sessionPolicy

	mu        sync.Mutex
	tokenData *domain.TokenData
	createdAt time.Time
	state     SessionState
	flight    *refreshFlight
}

type refreshFlight struct {
	done chan struct{}
	err  error
}

func newSession(login loginAdapter, now func() time.Time, policy sessionPolicy) *sessionImpl {
	return &sessionImpl{
		login:  login,
		now:    now,
		policy: policy,
		state:  SessionUninitialized,
	}
}

// NewSession creates the production AdvancedMD session owner.
func NewSession(creds Credentials, client *http.Client) Session {
	return newSession(newAdvancedMDLogin(creds, client), time.Now, sessionPolicy{
		staleAfter:   DefaultSessionStaleAfter,
		expiresAfter: DefaultSessionExpiresAfter,
		loginTimeout: DefaultSessionLoginTimeout,
	})
}

func (s *sessionImpl) Get(ctx context.Context) (*domain.TokenData, error) {
	if err := s.refresh(ctx, false); err != nil {
		s.mu.Lock()
		defer s.mu.Unlock()
		if s.usableLocked(s.now()) {
			return cloneTokenData(s.tokenData), nil
		}
		return nil, ErrSessionUnavailable
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	return cloneTokenData(s.tokenData), nil
}

func (s *sessionImpl) Maintain(ctx context.Context) error {
	return s.refresh(ctx, true)
}

func (s *sessionImpl) refresh(ctx context.Context, force bool) error {
	s.mu.Lock()
	if !force && s.statusLocked(s.now()).State == SessionFresh {
		s.mu.Unlock()
		return nil
	}
	if active := s.flight; active != nil {
		s.mu.Unlock()
		select {
		case <-active.done:
			return active.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	active := &refreshFlight{done: make(chan struct{})}
	s.flight = active
	s.mu.Unlock()

	loginCtx, cancel := context.WithTimeout(ctx, s.policy.loginTimeout)
	defer cancel()
	token, webserverURL, err := s.login.Authenticate(loginCtx)

	s.mu.Lock()
	defer s.mu.Unlock()
	active.err = err
	s.flight = nil
	close(active.done)
	if err != nil {
		if s.usableLocked(s.now()) {
			s.state = SessionDegraded
		} else {
			s.tokenData = nil
			s.createdAt = time.Time{}
			s.state = SessionUnavailable
		}
		return err
	}
	s.createdAt = s.now()
	s.tokenData = domain.BuildTokenDataAt(token, webserverURL, s.createdAt)
	s.state = SessionFresh
	return nil
}

func (s *sessionImpl) usableLocked(now time.Time) bool {
	return s.tokenData != nil && now.Sub(s.createdAt) < s.policy.expiresAfter
}

func (s *sessionImpl) Status() SessionStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.statusLocked(s.now())
}

func (s *sessionImpl) statusLocked(now time.Time) SessionStatus {
	status := SessionStatus{State: s.state}
	if s.tokenData != nil {
		status.TokenAge = max(now.Sub(s.createdAt), 0)
		status.ExpiresIn = max(s.policy.expiresAfter-status.TokenAge, 0)
	}
	if s.flight != nil {
		status.State = SessionRefreshing
		return status
	}
	if s.tokenData == nil {
		return status
	}
	if status.TokenAge >= s.policy.expiresAfter {
		status.State = SessionUnavailable
		return status
	}
	if s.state == SessionDegraded {
		status.State = SessionDegraded
		return status
	}
	if status.TokenAge >= s.policy.staleAfter {
		status.State = SessionStale
		return status
	}
	return status
}

func cloneTokenData(data *domain.TokenData) *domain.TokenData {
	if data == nil {
		return nil
	}
	copy := *data
	return &copy
}
