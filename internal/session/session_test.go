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
	if got := session.Status().State; got != SessionRefreshing {
		t.Fatalf("state during login = %q, want %q", got, SessionRefreshing)
	}
	close(release)
	if err := <-result; err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got := session.Status().State; got != SessionFresh {
		t.Fatalf("state after login = %q, want %q", got, SessionFresh)
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

func TestSessionUsesLastKnownGoodTokenWhenStaleRefreshFails(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		if loginCalls == 1 {
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		}
		return "", "", errors.New("temporary login failure")
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	clock.Advance(90 * time.Minute)
	if got := session.Status().State; got != SessionStale {
		t.Fatalf("state before failed refresh = %q, want %q", got, SessionStale)
	}

	token, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() with usable last-known-good token error = %v", err)
	}
	if token.Token != "Bearer known-good" {
		t.Fatalf("Get() token = %q, want last-known-good token", token.Token)
	}
	if got := session.Status().State; got != SessionDegraded {
		t.Fatalf("state after failed refresh = %q, want %q", got, SessionDegraded)
	}
}

func TestSessionReusesLastKnownGoodTokenBeforeDegradedRetry(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		switch loginCalls {
		case 1:
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		case 2:
			return "", "", errors.New("temporary login failure")
		default:
			return "recovered-too-soon", "https://provider.test/processrequest/api-801/app", nil
		}
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	clock.Advance(90 * time.Minute)
	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("Get() with failed refresh error = %v", err)
	}

	token, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() during degraded retry delay error = %v", err)
	}
	if token.Token != "Bearer known-good" {
		t.Fatalf("Get() during degraded retry delay token = %q, want last-known-good token", token.Token)
	}
}

func TestSessionRetriesDegradedAuthenticationAfterDelay(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		switch loginCalls {
		case 1:
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		case 2:
			return "", "", errors.New("temporary login failure")
		default:
			return "recovered", "https://provider.test/processrequest/api-801/app", nil
		}
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}
	clock.Advance(90 * time.Minute)
	if _, err := session.Get(context.Background()); err != nil {
		t.Fatalf("Get() with failed refresh error = %v", err)
	}
	clock.Advance(time.Minute)

	token, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() after degraded retry delay error = %v", err)
	}
	if token.Token != "Bearer recovered" {
		t.Fatalf("Get() after degraded retry delay token = %q, want recovered token", token.Token)
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
	if status.State != SessionFresh {
		t.Fatalf("state = %q, want %q", status.State, SessionFresh)
	}
	if status.TokenAge != 30*time.Minute {
		t.Fatalf("token age = %s, want 30m", status.TokenAge)
	}
	if status.ExpiresIn != 90*time.Minute {
		t.Fatalf("expires in = %s, want 90m", status.ExpiresIn)
	}
}

func TestSessionUsesControllableClockForCreationTime(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 34, 56, 0, time.UTC)
	clock := &testClock{now: now}
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		return "session-token", "https://provider.test/processrequest/api-801/app", nil
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})

	token, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if token.CreatedAt != "2026-07-24T12:34:56Z" {
		t.Fatalf("CreatedAt = %q, want controllable clock time", token.CreatedAt)
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

	token, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("recovery Get() error = %v", err)
	}
	if token.Token != "Bearer recovered-token" {
		t.Fatalf("recovery token = %q", token.Token)
	}
	if got := session.Status().State; got != SessionFresh {
		t.Fatalf("state after recovery = %q, want %q", got, SessionFresh)
	}
}

func TestSessionMaintenanceFailureKeepsUsableSessionDegraded(t *testing.T) {
	now := time.Date(2026, time.July, 24, 12, 0, 0, 0, time.UTC)
	clock := &testClock{now: now}
	loginCalls := 0
	var session Session = newSession(loginAdapterFunc(func(context.Context) (string, string, error) {
		loginCalls++
		if loginCalls == 1 {
			return "known-good", "https://provider.test/processrequest/api-801/app", nil
		}
		return "", "", errors.New("temporary maintenance failure")
	}), clock.Now, sessionPolicy{
		staleAfter:   time.Hour,
		expiresAfter: 2 * time.Hour,
		loginTimeout: time.Minute,
	})
	original, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("initial Get() error = %v", err)
	}

	if err := session.Maintain(context.Background()); err == nil {
		t.Fatal("Maintain() error = nil, want refresh failure")
	}
	if got := session.Status().State; got != SessionDegraded {
		t.Fatalf("state after maintenance failure = %q, want %q", got, SessionDegraded)
	}

	afterFailure, err := session.Get(context.Background())
	if err != nil {
		t.Fatalf("Get() after maintenance failure error = %v", err)
	}
	if afterFailure.Token != original.Token {
		t.Fatalf("maintenance failure replaced last-known-good token: %q", afterFailure.Token)
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
	}, client)

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
