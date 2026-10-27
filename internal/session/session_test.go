package session

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSessionRequestTimeAuthenticationTransitionsToFresh(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	started := make(chan struct{})
	release := make(chan struct{})
	login := loginAdapterFunc(func(ctx context.Context) (string, string, error) {
		close(started)
		select {
		case <-release:
			return "session-token", "https://provider.test/processrequest/api-801/app", nil
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	})
	var session Session = newSession(login, clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	if got := session.Status().State; got != SessionUninitialized {
		t.Fatalf("initial state = %q, want %q", got, SessionUninitialized)
	}

	result := make(chan error, 1)
	go func() {
		_, err := session.Get(context.Background())
		result <- err
	}()

	<-started
	if got := session.Status().State; got != sessionRefreshing {
		t.Fatalf("state during login = %q, want %q", got, sessionRefreshing)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := session.Status().State; got != sessionFresh {
		t.Fatalf("state after login = %q, want %q", got, sessionFresh)
	}
}

func TestSessionReusesFreshSessionAndProtectsCachedState(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		return "session-token", "https://provider.test/processrequest/api-801/app", nil
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	first, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("first Get() error = %v", err)
	}
	first.Token = "caller mutation"

	second, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("second Get() error = %v", err)
	}
	if loginCalls != 1 {
		t.Fatalf("login calls = %d, want 1", loginCalls)
	}
	if second.Token != "Bearer session-token" {
		t.Fatalf("cached token changed through caller copy: %q", second.Token)
	}
}

func TestSessionReusesDocumentedFreshTokenBefore20Hours(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		if loginCalls == 1 {
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		}
		return "refreshed-too-soon", "https://provider.test/processrequest/api-801/app", nil
	}), clock.Now, sessionPolicy{
		staleAfter:   defaultSessionStaleAfter,
		expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute,
	})

	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	clock.Advance(20*time.Hour - time.Minute)

	token, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() before 20 hours error = %v", err)
	}
	if token.Token != "Bearer known-good" {
		t.Fatalf("Get() before 20 hours token = %q, want current token", token.Token)
	}

	clock.Advance(time.Minute)
	if got := session.Status().State; got != sessionStale {
		t.Fatalf("state at 20 hours = %q, want %q", got, sessionStale)
	}
}

func TestSessionUsesLastKnownGoodUntilDocumented24HourExpiration(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		if loginCalls == 1 {
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		}
		return "", "", errors.New("temporary login failure")
	}), clock.Now, sessionPolicy{
		staleAfter:   defaultSessionStaleAfter,
		expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute,
	})

	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	clock.Advance(24*time.Hour - time.Minute)

	token, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() before documented expiration error = %v", err)
	}
	if token.Token != "Bearer known-good" {
		t.Fatalf("Get() before documented expiration token = %q, want last-known-good token", token.Token)
	}

	clock.Advance(time.Minute)
	token, err = session.Get(context.Background())
	if token != nil {
		t.Fatal("Get() returned token at documented expiration")
	}
	if !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("Get() at documented expiration error = %v, want unavailable", err)
	}
}

func TestSessionConcurrentRequestsShareOneLogin(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	started := make(chan struct{})
	release := make(chan struct{})
	var loginCalls atomic.Int32
	var session Session = newSession(loginAdapterFunc(func(ctx context.Context) (string, string, error) {
		if loginCalls.Add(1) == 1 {
			close(started)
		}
		select {
		case <-release:
			return "shared-token", "https://provider.test/processrequest/api-801/app", nil
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	const callers = 32
	start := make(chan struct{})
	results := make(chan error, callers)
	var ready sync.WaitGroup
	ready.Add(callers)
	for range callers {
		go func() {
			ready.Done()
			<-start
			token, err := session.Get(context.Background())
			if err == nil && token.Token != "Bearer shared-token" {
				err = &unexpectedTokenError{got: token.Token}
			}
			results <- err
		}()
	}
	ready.Wait()
	close(start)
	<-started
	close(release)

	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("concurrent Get() error = %v", err)
		}
	}
	if got := loginCalls.Load(); got != 1 {
		t.Fatalf("login calls = %d, want 1", got)
	}
}

func TestSessionRequestFallbackRecoversAfterFailedMaintenance(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	session := newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		switch loginCalls {
		case 1:
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		case 2:
			return "", "", errors.New("scheduled maintenance failed")
		default:
			return "request-recovered", "https://provider.test/processrequest/api-801/app", nil
		}
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
		retryDelay:   time.Minute,
	})

	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	if err := session.Maintain(context.Background()); err == nil {
		t.Fatal("Maintain() error = nil, want scheduled failure")
	}

	clock.Advance(time.Minute)
	token, err := session.Get(context.Background())
	if err != nil || token.Token != "Bearer known-good" {
		t.Fatalf("Get() during request refresh = %v, %v; want last-known-good token", token, err)
	}
	waitForRefresh(session)
	token, err = session.Get(context.Background())
	if err != nil {
		t.Fatalf("request fallback Get() error = %v", err)
	}
	if token.Token != "Bearer request-recovered" {
		t.Fatalf("request fallback token = %q", token.Token)
	}
	if loginCalls != 3 {
		t.Fatalf("login calls = %d, want 3", loginCalls)
	}
}

func TestSessionStatusReportsSafeTimingWithoutSessionData(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		return "session-token", "https://provider.test/processrequest/api-801/app", nil
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})
	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("Get() error = %v", err)
	}

	clock.Advance(30 * time.Minute)
	status := session.Status()
	if status.State != sessionFresh {
		t.Fatalf("state = %q, want %q", status.State, sessionFresh)
	}
	if status.TokenAge != 30*time.Minute {
		t.Fatalf("token age = %s, want 30m", status.TokenAge)
	}
	if status.ExpiresIn != 90*time.Minute {
		t.Fatalf("expires in = %s, want 90m", status.ExpiresIn)
	}
}

func TestSessionHardExpirationIncludesAuthenticationTime(t *testing.T) {
	now := time.Date(2026, time.July, 25, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		if loginCalls == 1 {
			clock.Advance(2 * time.Minute)
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		}
		return "", "", errors.New("temporary login failure")
	}), clock.Now, sessionPolicy{
		staleAfter:   defaultSessionStaleAfter,
		expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute,
	})

	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	clock.Advance(24*time.Hour - 2*time.Minute)

	token, err := session.Get(context.Background())
	if token != nil {
		t.Fatal("Get() returned token 24 hours after authentication started")
	}
	if !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("Get() error = %v, want unavailable", err)
	}
}

func TestSessionReturnsStableUnavailableAfterHardExpiration(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		if loginCalls == 1 {
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		}
		return "", "", errors.New("provider detail that must not escape")
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})
	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}

	clock.Advance(2 * time.Hour)
	token, err := session.Get(context.Background())
	if token != nil {
		t.Fatal("Get() returned token after hard expiration")
	}
	if !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("Get() error = %v, want stable unavailable error", err)
	}
	if err.Error() != "advancedmd session unavailable" {
		t.Fatalf("unavailable error leaked provider detail: %q", err)
	}
	if got := session.Status().State; got != SessionUnavailable {
		t.Fatalf("state = %q, want %q", got, SessionUnavailable)
	}
}

func TestSessionRetriesAfterUnavailableAuthentication(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		if loginCalls == 1 {
			return "", "", errors.New("temporary login failure")
		}
		return "recovered-token", "https://provider.test/processrequest/api-801/app", nil
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	if _, err := session.Get(context.Background()); !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("first Get() error = %v, want unavailable", err)
	}
	if got := session.Status().State; got != SessionUnavailable {
		t.Fatalf("state after failed initial login = %q, want %q", got, SessionUnavailable)
	}

	clock.Advance(defaultSessionRetryDelay)

	token, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("recovery Get() error = %v", err)
	}
	if token.Token != "Bearer recovered-token" {
		t.Fatalf("recovery token = %q", token.Token)
	}
	if got := session.Status().State; got != sessionFresh {
		t.Fatalf("state after recovery = %q, want %q", got, sessionFresh)
	}
}

func TestSessionFreshGetDoesNotWaitForMaintenance(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	maintenanceStarted := make(chan struct{})
	releaseMaintenance := make(chan struct{})
	var loginCalls atomic.Int32
	var session Session = newSession(loginAdapterFunc(func(ctx context.Context) (string, string, error) {
		if loginCalls.Add(1) == 1 {
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		}
		close(maintenanceStarted)
		select {
		case <-releaseMaintenance:
			return "refreshed", "https://provider.test/processrequest/api-801/app", nil
		case <-ctx.Done():
			return "", "", ctx.Err()
		}
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	maintenanceDone := make(chan error, 1)
	go func() {
		maintenanceDone <- session.Maintain(context.Background())
	}()
	<-maintenanceStarted

	type getResult struct {
		token string
		err   error
	}
	getDone := make(chan getResult, 1)
	go func() {
		token, err := session.Get(context.Background())
		result := getResult{err: err}
		if token != nil {
			result.token = token.Token
		}
		getDone <- result
	}()

	select {
	case result := <-getDone:
		close(releaseMaintenance)
		if err := <-maintenanceDone; err != nil {
			t.Fatalf("Maintain() error = %v", err)
		}
		if result.err != nil {
			t.Fatalf("Get() during maintenance error = %v", result.err)
		}
		if result.token != "Bearer known-good" {
			t.Fatalf("Get() during maintenance token = %q, want current fresh token", result.token)
		}
	case <-time.After(time.Second):
		close(releaseMaintenance)
		<-maintenanceDone
		t.Fatal("Get() waited for maintenance despite a fresh cached token")
	}
}

func TestSessionBoundsRequestTimeAuthentication(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	sawDeadline := false
	var session Session = newSession(loginAdapterFunc(func(ctx context.Context) (string, string, error) {
		_, sawDeadline = ctx.Deadline()
		return "", "", context.DeadlineExceeded
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	if _, err := session.Get(context.Background()); !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("Get() error = %v, want stable unavailable", err)
	}
	if !sawDeadline {
		t.Fatal("login adapter did not receive a bounded context")
	}
}

func TestNewSessionDoesNotAuthenticateUntilRequested(t *testing.T) {
	var calls atomic.Int32
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)
		return nil, errors.New("unexpected login")
	})}

	session := NewSession(Credentials{
		Username:  "user",
		Password:  "password",
		OfficeKey: "office",
		AppName:   "app",
	}, client, time.Now)

	if got := session.Status().State; got != SessionUninitialized {
		t.Fatalf("state = %q, want %q", got, SessionUninitialized)
	}
	if got := calls.Load(); got != 0 {
		t.Fatalf("startup login calls = %d, want 0", got)
	}
}

type testClock struct {
	now time.Time
}

func (c *testClock) Now() time.Time {
	return c.now
}

func (c *testClock) Advance(d time.Duration) {
	c.now = c.now.Add(d)
}

type loginAdapterFunc func(context.Context) (string, string, error)

func (f loginAdapterFunc) Authenticate(ctx context.Context) (string, string, error) {
	return f(ctx)
}

type unexpectedTokenError struct {
	got string
}

func (e *unexpectedTokenError) Error() string {
	return "unexpected token: " + e.got
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestUnavailableSessionHonorsRetryCooldown(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)}
	calls := 0
	s := newSession(loginAdapterFunc(func(context.Context) (string, string, error) { calls++; return "", "", errors.New("offline") }), clock.Now, sessionPolicy{loginTimeout: time.Second, retryDelay: time.Minute})
	for i := 0; i < 10; i++ {
		if _, err := s.Get(context.Background()); !errors.Is(err, ErrSessionUnavailable) {
			t.Fatalf("error=%v", err)
		}
	}
	if calls != 1 {
		t.Fatalf("login attempts=%d, want 1", calls)
	}
	clock.Advance(time.Minute)
	s.Get(context.Background())
	if calls != 2 {
		t.Fatalf("login attempts after cooldown=%d, want 2", calls)
	}
}

func TestCanceledCallerDoesNotCancelSharedLogin(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 9, 14, 0, 0, 0, 0, time.UTC)}
	started := make(chan struct{})
	release := make(chan struct{})
	loginCanceled := make(chan bool, 1)
	var calls atomic.Int32
	s := newSession(loginAdapterFunc(func(ctx context.Context) (string, string, error) {
		calls.Add(1)
		close(started)
		select {
		case <-release:
			loginCanceled <- false
			return "recovered-token", "https://provider.test/processrequest/api-801/app", nil
		case <-ctx.Done():
			loginCanceled <- true
			return "", "", ctx.Err()
		}
	}), clock.Now, sessionPolicy{
		staleAfter: defaultSessionStaleAfter, expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute, retryDelay: time.Minute,
	})

	leaderCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	leader := make(chan error, 1)
	go func() { _, err := s.Get(leaderCtx); leader <- err }()
	<-started
	waiter := make(chan error, 1)
	go func() {
		token, err := s.Get(context.Background())
		if err == nil && token.Token != "Bearer recovered-token" {
			err = &unexpectedTokenError{got: token.Token}
		}
		waiter <- err
	}()
	cancel()
	if err := <-leader; !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("canceled caller error = %v, want session unavailable", err)
	}

	close(release)
	if <-loginCanceled {
		t.Fatal("shared login was canceled with the leading caller")
	}
	if err := <-waiter; err != nil {
		t.Fatalf("waiting caller failed after leader cancellation: %v", err)
	}
	token, err := s.Get(context.Background())
	if err != nil || token.Token != "Bearer recovered-token" {
		t.Fatalf("Get() after shared login = %v, %v; want recovered token", token, err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("login attempts = %d, want 1", got)
	}
}

func TestSessionRejectedCurrentTokenLogsInAgain(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	login := &countingLogin{tokens: []string{"revoked", "replacement"}}
	s := newSession(login, clock.Now, sessionPolicy{
		staleAfter: defaultSessionStaleAfter, expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute, retryDelay: time.Minute,
	})

	first, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	s.Reject(first)

	token, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() after rejection error = %v", err)
	}
	if token.Token != "Bearer replacement" {
		t.Fatalf("Get() after rejection token = %q, want replacement", token.Token)
	}
	if got := login.calls.Load(); got != 2 {
		t.Fatalf("login calls = %d, want 2", got)
	}
}

func TestSessionRejectionDuringRetryDelayLogsInAgain(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	var calls atomic.Int32
	s := newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		switch calls.Add(1) {
		case 1:
			return "revoked", "https://provider.test/processrequest/api-801/app", nil
		case 2:
			return "", "", errors.New("scheduled maintenance failed")
		default:
			return "replacement", "https://provider.test/processrequest/api-801/app", nil
		}
	}), clock.Now, sessionPolicy{
		staleAfter: defaultSessionStaleAfter, expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute, retryDelay: time.Minute,
	})
	revoked, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	if err := s.Maintain(context.Background()); err == nil {
		t.Fatal("Maintain() error = nil, want scheduled failure")
	}
	s.Reject(revoked)

	token, err := s.Get(context.Background())
	if err != nil || token.Token != "Bearer replacement" {
		t.Fatalf("Get() after rejection during retry delay = %v, %v; want replacement token", token, err)
	}
}

func TestSessionIgnoresRejectionOfReplacedToken(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	login := &countingLogin{tokens: []string{"old", "current", "unexpected"}}
	s := newSession(login, clock.Now, sessionPolicy{
		staleAfter: defaultSessionStaleAfter, expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute, retryDelay: time.Minute,
	})

	old, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	if err := s.Maintain(context.Background()); err != nil {
		t.Fatalf("Maintain() error = %v", err)
	}
	s.Reject(old)

	token, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if token.Token != "Bearer current" {
		t.Fatalf("Get() token = %q, want current", token.Token)
	}
	if got := login.calls.Load(); got != 2 {
		t.Fatalf("login calls = %d, want 2", got)
	}
}

func TestSessionConcurrentRejectionsShareOneLogin(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	login := &countingLogin{tokens: []string{"revoked", "replacement", "unexpected"}}
	s := newSession(login, clock.Now, sessionPolicy{
		staleAfter: defaultSessionStaleAfter, expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute, retryDelay: time.Minute,
	})
	revoked, err := s.Get(context.Background())
	if err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}

	const callers = 32
	results := make(chan error, callers)
	for range callers {
		go func() {
			s.Reject(revoked)
			token, err := s.Get(context.Background())
			if err == nil && token.Token != "Bearer replacement" {
				err = &unexpectedTokenError{got: token.Token}
			}
			results <- err
		}()
	}
	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("Get() after concurrent rejection error = %v", err)
		}
	}
	if got := login.calls.Load(); got != 2 {
		t.Fatalf("login calls = %d, want 2", got)
	}
}

func TestSessionServesStaleTokenWithoutWaitingForRefresh(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	refreshStarted := make(chan struct{})
	releaseRefresh := make(chan struct{})
	refreshCanceled := make(chan bool, 1)
	var calls atomic.Int32
	s := newSession(loginAdapterFunc(func(ctx context.Context) (string, string, error) {
		if calls.Add(1) == 1 {
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		}
		close(refreshStarted)
		select {
		case <-releaseRefresh:
			refreshCanceled <- false
			return "refreshed", "https://provider.test/processrequest/api-801/app", nil
		case <-ctx.Done():
			refreshCanceled <- true
			return "", "", ctx.Err()
		}
	}), clock.Now, sessionPolicy{
		staleAfter: defaultSessionStaleAfter, expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute, retryDelay: time.Minute,
	})
	if _, err := s.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	clock.Advance(defaultSessionStaleAfter)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	token, err := s.Get(ctx)
	if ctx.Err() != nil {
		t.Fatalf("Get() with a usable stale token waited until the caller deadline")
	}
	cancel()
	if err != nil || token.Token != "Bearer known-good" {
		t.Fatalf("Get() = %v, %v; want last-known-good token", token, err)
	}
	<-refreshStarted

	const callers = 16
	results := make(chan error, callers)
	for range callers {
		go func() {
			token, err := s.Get(context.Background())
			if err == nil && token.Token != "Bearer known-good" {
				err = &unexpectedTokenError{got: token.Token}
			}
			results <- err
		}()
	}
	for range callers {
		if err := <-results; err != nil {
			t.Fatalf("Get() during refresh error = %v", err)
		}
	}
	if _, err := s.Get(context.Background()); err != nil {
		t.Fatalf("back-to-back Get() during refresh error = %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("login calls during refresh = %d, want 2", got)
	}

	close(releaseRefresh)
	if <-refreshCanceled {
		t.Fatal("background refresh was canceled with the caller")
	}
	waitForRefresh(s)
	token, err = s.Get(context.Background())
	if err != nil || token.Token != "Bearer refreshed" {
		t.Fatalf("Get() after refresh = %v, %v; want refreshed token", token, err)
	}
}

func TestSessionFailedBackgroundRefreshWaitsForRetryDelay(t *testing.T) {
	clock := &testClock{now: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	var calls atomic.Int32
	s := newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		switch calls.Add(1) {
		case 1:
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		case 2:
			return "", "", errors.New("temporary login failure")
		default:
			return "recovered", "https://provider.test/processrequest/api-801/app", nil
		}
	}), clock.Now, sessionPolicy{
		staleAfter: defaultSessionStaleAfter, expiresAfter: defaultSessionExpiresAfter,
		loginTimeout: time.Minute, retryDelay: time.Minute,
	})
	if _, err := s.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	clock.Advance(defaultSessionStaleAfter)
	if _, err := s.Get(context.Background()); err != nil {
		t.Fatalf("stale Get() error = %v", err)
	}
	waitForRefresh(s)
	if got := s.Status().State; got != sessionDegraded {
		t.Fatalf("state after failed refresh = %q, want %q", got, sessionDegraded)
	}

	clock.Advance(time.Minute - time.Second)
	token, err := s.Get(context.Background())
	if err != nil || token.Token != "Bearer known-good" {
		t.Fatalf("Get() before retry delay = %v, %v; want last-known-good token", token, err)
	}
	waitForRefresh(s)
	if got := calls.Load(); got != 2 {
		t.Fatalf("login calls before retry delay = %d, want 2", got)
	}

	clock.Advance(time.Second)
	if _, err := s.Get(context.Background()); err != nil {
		t.Fatalf("Get() at retry delay error = %v", err)
	}
	waitForRefresh(s)
	if got := calls.Load(); got != 3 {
		t.Fatalf("login calls at retry delay = %d, want 3", got)
	}
	token, err = s.Get(context.Background())
	if err != nil || token.Token != "Bearer recovered" {
		t.Fatalf("Get() after retry = %v, %v; want recovered token", token, err)
	}
}

type countingLogin struct {
	tokens []string
	calls  atomic.Int32
}

func (l *countingLogin) Authenticate(context.Context) (string, string, error) {
	call := int(l.calls.Add(1))
	return l.tokens[min(call, len(l.tokens))-1], "https://provider.test/processrequest/api-801/app", nil
}

func waitForRefresh(s *sessionImpl) {
	s.mu.Lock()
	flight := s.flight
	s.mu.Unlock()
	if flight != nil {
		<-flight.done
	}
}
