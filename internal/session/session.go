package session

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

type Session interface {
	Get(context.Context) (*TokenData, error)
	Maintain(context.Context) error
	Status() SessionStatus
}

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
	DefaultSessionExpiresAfter = 24 * time.Hour
	DefaultSessionStaleAfter   = 20 * time.Hour
	DefaultSessionLoginTimeout = 50 * time.Second
	DefaultSessionRetryDelay   = time.Minute
)

var ErrSessionUnavailable = errors.New("advancedmd session unavailable")

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
	done           chan struct{}
	err            error
	callerCanceled bool
}

func newSession(login loginAdapter, now func() time.Time, policy sessionPolicy) *sessionImpl {
	if policy.retryDelay <= 0 {
		policy.retryDelay = DefaultSessionRetryDelay
	}
	return &sessionImpl{
		login:  login,
		now:    now,
		policy: policy,
		state:  SessionUninitialized,
	}
}

func NewSession(creds Credentials, client *http.Client) Session {
	return newSession(newAdvancedMDLogin(creds, client), time.Now, sessionPolicy{
		staleAfter:   DefaultSessionStaleAfter,
		expiresAfter: DefaultSessionExpiresAfter,
		loginTimeout: DefaultSessionLoginTimeout,
		retryDelay:   DefaultSessionRetryDelay,
	})
}

func (s *sessionImpl) Get(ctx context.Context) (*TokenData, error) {
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
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		s.mu.Lock()
		if !force {
			now := s.now()
			if s.freshLocked(now) {
				s.mu.Unlock()
				return nil
			}
			if now.Before(s.retryAt) {
				usable := s.usableLocked(now)
				s.mu.Unlock()
				if usable {
					return nil
				}
				return ErrSessionUnavailable
			}
		}
		active := s.flight
		if active == nil {
			return s.authenticateLocked(ctx)
		}
		s.mu.Unlock()
		select {
		case <-active.done:
			if active.callerCanceled {
				continue
			}
			return active.err
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func (s *sessionImpl) authenticateLocked(ctx context.Context) error {
	active := &refreshFlight{done: make(chan struct{})}
	s.flight = active
	loginStartedAt := s.now()
	s.mu.Unlock()

	loginCtx, cancel := context.WithTimeout(ctx, s.policy.loginTimeout)
	defer cancel()
	token, webserverURL, err := s.login.Authenticate(loginCtx)

	s.mu.Lock()
	defer s.mu.Unlock()
	active.err = err
	active.callerCanceled = err != nil && ctx.Err() != nil
	s.flight = nil
	close(active.done)
	if err != nil {
		now := s.now()
		s.retryAt = time.Time{}
		if ctx.Err() == nil {
			s.retryAt = now.Add(s.policy.retryDelay)
		}
		if s.usableLocked(now) {
			s.state = SessionDegraded
		} else {
			s.tokenData = nil
			s.createdAt = time.Time{}
			s.state = SessionUnavailable
		}
		return err
	}
	s.createdAt = loginStartedAt
	s.tokenData = BuildTokenData(token, webserverURL)
	s.retryAt = time.Time{}
	s.state = SessionFresh
	return nil
}

func (s *sessionImpl) usableLocked(now time.Time) bool {
	return s.tokenData != nil && now.Sub(s.createdAt) < s.policy.expiresAfter
}

func (s *sessionImpl) freshLocked(now time.Time) bool {
	return s.state == SessionFresh &&
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

func cloneTokenData(data *TokenData) *TokenData {
	if data == nil {
		return nil
	}
	copy := *data
	return &copy
}
