package auth

import (
	"context"
	"log"
	"sync"
	"time"

	"advancedmd-token-management/internal/domain"
	"advancedmd-token-management/internal/safeerrors"
)

const (
	// refreshInterval is how often the background refresh runs (20 hours)
	refreshInterval = 20 * time.Hour
)

// TokenManager handles in-memory token caching and background refresh.
type TokenManager struct {
	authenticator *Authenticator

	mu        sync.RWMutex
	tokenData *domain.TokenData

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewTokenManager creates a new TokenManager.
func NewTokenManager(auth *Authenticator) *TokenManager {
	return &TokenManager{
		authenticator: auth,
		stopCh:        make(chan struct{}),
	}
}

// Start authenticates with AMD and begins the background refresh goroutine.
func (tm *TokenManager) Start(ctx context.Context) error {
	if err := tm.refresh(ctx); err != nil {
		return err
	}

	tm.wg.Add(1)
	go tm.backgroundRefresh()

	return nil
}

// Stop gracefully stops the background refresh.
func (tm *TokenManager) Stop() {
	close(tm.stopCh)
	tm.wg.Wait()
}

// GetToken returns the current token data.
// If no token is cached, it performs an on-demand refresh.
func (tm *TokenManager) GetToken(ctx context.Context) (*domain.TokenData, error) {
	tm.mu.RLock()
	data := tm.tokenData
	tm.mu.RUnlock()

	if data != nil {
		return data, nil
	}

	if err := tm.refresh(ctx); err != nil {
		return nil, err
	}

	tm.mu.RLock()
	defer tm.mu.RUnlock()
	return tm.tokenData, nil
}

// refresh performs authentication and updates the in-memory cache.
func (tm *TokenManager) refresh(ctx context.Context) error {
	log.Println("Refreshing AdvancedMD token...")

	token, webserverURL, err := tm.authenticator.Authenticate()
	if err != nil {
		return err
	}

	data := domain.BuildTokenData(token, webserverURL)

	tm.mu.Lock()
	tm.tokenData = data
	tm.mu.Unlock()

	log.Printf("Token refreshed successfully (created: %s)", data.CreatedAt)
	return nil
}

// backgroundRefresh runs the periodic token refresh.
func (tm *TokenManager) backgroundRefresh() {
	defer tm.wg.Done()

	ticker := time.NewTicker(refreshInterval)
	defer ticker.Stop()

	for {
		select {
		case <-tm.stopCh:
			log.Println("Background token refresh stopped")
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			if err := tm.refresh(ctx); err != nil {
				log.Printf("Background token refresh failed: category=%s", safeerrors.Classify(err))
			}
			cancel()
		}
	}
}
