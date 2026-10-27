package session

import (
	"context"
	"log"
	"net/http"
	"sync"
	"time"

	"advancedmd-token-management/internal/safeerrors"
)

type Session interface {
	Get(context.Context) (*TokenData, error)
	Maintain(context.Context) error
	Reject(*TokenData)
	Status() SessionStatus
}

type SessionState string

const (
	SessionUninitialized SessionState = "uninitialized"
	sessionRefreshing    SessionState = "refreshing"
	sessionFresh         SessionState = "fresh"
	sessionStale         SessionState = "stale"
	sessionDegraded      SessionState = "degraded"
	SessionUnavailable   SessionState = "unavailable"
)

const (
	defaultSessionExpiresAfter = 24 * time.Hour
	defaultSessionStaleAfter   = 20 * time.Hour
	DefaultSessionLoginTimeout = 50 * time.Second
	defaultSessionRetryDelay   = time.Minute
)

var ErrSessionUnavailable = safeerrors.New(safeerrors.CategoryUnavailable, "advancedmd session unavailable")

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
	retryDelay   time.Duration
}

type sessionImpl struct {
	login  loginAdapter
	now    func() time.Time
	policy sessionPolicy

	mu        sync.Mutex
	tokenData *TokenData
	createdAt time.Time
	retryAt   time.Time
	state     SessionState
	flight    *refreshFlight
}

type refreshFlight struct {
	done chan struct{}
	err  error
}

func newSession(login loginAdapter, now func() time.Time, policy sessionPolicy) *sessionImpl {
	if policy.retryDelay <= 0 {
		policy.retryDelay = defaultSessionRetryDelay
	}
	return &sessionImpl{
		login:  login,
		now:    now,
		policy: policy,
		state:  SessionUninitialized,
	}
}

func NewSession(creds Credentials, client *http.Client, now func() time.Time) Session {
	return newSession(newAdvancedMDLogin(creds, client, now), now, sessionPolicy{
		staleAfter:   defaultSessionStaleAfter,
		expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: DefaultSessionLoginTimeout,
		retryDelay:   defaultSessionRetryDelay,
	})
}

func (s *sessionImpl) Get(ctx context.Context) (*TokenData, error) {
	s.mu.Lock()
	now := s.now()
	if s.usableLocked(now) {
		if !s.freshLocked(now) && !now.Before(s.retryAt) {
			s.startLoginLocked(ctx)
		}
		token := cloneTokenData(s.tokenData)
		s.mu.Unlock()
		return token, nil
	}
	if now.Before(s.retryAt) {
		s.mu.Unlock()
		return nil, ErrSessionUnavailable
	}
	flight := s.startLoginLocked(ctx)
	s.mu.Unlock()

	if err := waitForLogin(ctx, flight); err != nil {
		return nil, ErrSessionUnavailable
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.usableLocked(s.now()) {
		return nil, ErrSessionUnavailable
	}
	return cloneTokenData(s.tokenData), nil
}

func (s *sessionImpl) Maintain(ctx context.Context) error {
	s.mu.Lock()
	flight := s.startLoginLocked(ctx)
	s.mu.Unlock()
	return waitForLogin(ctx, flight)
}

func (s *sessionImpl) Reject(rejected *TokenData) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if rejected == nil || s.tokenData == nil || *rejected != *s.tokenData {
		return
	}
	s.tokenData = nil
	s.createdAt = time.Time{}
	s.retryAt = time.Time{}
	s.state = SessionUnavailable
	log.Printf("WARNING: provider=advancedmd session token rejected; next request logs in again")
}

func (s *sessionImpl) startLoginLocked(ctx context.Context) *refreshFlight {
	if s.flight != nil {
		return s.flight
	}
	flight := &refreshFlight{done: make(chan struct{})}
	s.flight = flight
	loginStartedAt := s.now()
	loginCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.policy.loginTimeout)
	go func() {
		defer cancel()
		token, webserverURL, err := s.login.Authenticate(loginCtx)
		s.finishLogin(flight, loginStartedAt, token, webserverURL, err)
	}()
	return flight
}

func (s *sessionImpl) finishLogin(flight *refreshFlight, loginStartedAt time.Time, token, webserverURL string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	defer close(flight.done)
	flight.err = err
	s.flight = nil
	if err != nil {
		now := s.now()
		s.retryAt = now.Add(s.policy.retryDelay)
		if s.usableLocked(now) {
			s.state = sessionDegraded
		} else {
			s.tokenData = nil
			s.createdAt = time.Time{}
			s.state = SessionUnavailable
		}
		return
	}
	s.createdAt = loginStartedAt
	s.tokenData = buildTokenData(token, webserverURL)
	s.retryAt = time.Time{}
	s.state = sessionFresh
}

func waitForLogin(ctx context.Context, flight *refreshFlight) error {
	select {
	case <-flight.done:
		return flight.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *sessionImpl) usableLocked(now time.Time) bool {
	return s.tokenData != nil && now.Sub(s.createdAt) < s.policy.expiresAfter
}

func (s *sessionImpl) freshLocked(now time.Time) bool {
	return s.state == sessionFresh &&
		s.tokenData != nil &&
		now.Sub(s.createdAt) < s.policy.staleAfter
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
		status.State = sessionRefreshing
		return status
	}
	if s.tokenData == nil {
		return status
	}
	if status.TokenAge >= s.policy.expiresAfter {
		status.State = SessionUnavailable
		return status
	}
	if s.state == sessionDegraded {
		status.State = sessionDegraded
		return status
	}
	if status.TokenAge >= s.policy.staleAfter {
		status.State = sessionStale
		return status
	}
	return status
}

func cloneTokenData(data *TokenData) *TokenData {
	if data == nil {
		return nil
	}
	copy := *data
	return &copy
}
