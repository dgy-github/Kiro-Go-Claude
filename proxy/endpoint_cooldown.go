package proxy

import (
	"errors"
	"fmt"
	"kiro-go/config"
	"kiro-go/logger"
	"strings"
	"sync"
	"time"
)

const (
	endpointQuotaBaseCooldown     = 30 * time.Second
	endpointQuotaMaxCooldown      = 10 * time.Minute
	endpointTransientBaseCooldown = 10 * time.Second
	endpointTransientMaxCooldown  = 2 * time.Minute
)

type endpointCooldownState struct {
	failures int
	until    time.Time
	reason   string
}

type endpointCooldownRegistry struct {
	mu      sync.Mutex
	entries map[string]endpointCooldownState
}

type endpointCooldownError struct {
	AccountEmail string
	RetryAfter   time.Duration
	Endpoints    []string
}

func (e *endpointCooldownError) Error() string {
	retry := e.RetryAfter.Round(time.Second)
	if retry < time.Second && e.RetryAfter > 0 {
		retry = time.Second
	}
	return fmt.Sprintf("all Kiro endpoints are cooling down for account %s; retry after %s", e.AccountEmail, retry)
}

func isEndpointCooldownError(err error) bool {
	var cooldownErr *endpointCooldownError
	return errors.As(err, &cooldownErr)
}

type kiroQuotaError struct {
	Endpoints []string
}

func (e *kiroQuotaError) Error() string {
	return fmt.Sprintf("quota exhausted (429) on endpoints: %s", strings.Join(e.Endpoints, ", "))
}

var kiroEndpointCooldowns = newEndpointCooldownRegistry()

func newEndpointCooldownRegistry() *endpointCooldownRegistry {
	return &endpointCooldownRegistry{entries: make(map[string]endpointCooldownState)}
}

func resetEndpointCooldownsForTest() {
	kiroEndpointCooldowns = newEndpointCooldownRegistry()
}

func availableKiroEndpoints(account *config.Account, endpoints []kiroEndpoint) ([]kiroEndpoint, time.Duration, []string) {
	return kiroEndpointCooldowns.available(account, endpoints)
}

func recordKiroEndpointQuotaFailure(account *config.Account, ep kiroEndpoint) {
	kiroEndpointCooldowns.recordFailure(account, ep, "429 quota", endpointQuotaBaseCooldown, endpointQuotaMaxCooldown)
}

func recordKiroEndpointTransientFailure(account *config.Account, ep kiroEndpoint, err error) {
	reason := "transient failure"
	if err != nil {
		reason = err.Error()
	}
	kiroEndpointCooldowns.recordFailure(account, ep, reason, endpointTransientBaseCooldown, endpointTransientMaxCooldown)
}

func recordKiroEndpointSuccess(account *config.Account, ep kiroEndpoint) {
	kiroEndpointCooldowns.recordSuccess(account, ep)
}

func (r *endpointCooldownRegistry) available(account *config.Account, endpoints []kiroEndpoint) ([]kiroEndpoint, time.Duration, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	available := make([]kiroEndpoint, 0, len(endpoints))
	cooling := make([]string, 0)
	var soonest time.Time

	for _, ep := range endpoints {
		key := endpointCooldownKey(account, ep)
		state, ok := r.entries[key]
		if ok && now.Before(state.until) {
			cooling = append(cooling, ep.Name)
			if soonest.IsZero() || state.until.Before(soonest) {
				soonest = state.until
			}
			continue
		}
		if ok {
			delete(r.entries, key)
		}
		available = append(available, ep)
	}

	if soonest.IsZero() {
		return available, 0, cooling
	}
	return available, time.Until(soonest), cooling
}

func (r *endpointCooldownRegistry) recordFailure(account *config.Account, ep kiroEndpoint, reason string, base, max time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()

	key := endpointCooldownKey(account, ep)
	state := r.entries[key]
	state.failures++
	delay := cooldownDelay(state.failures, base, max)
	state.until = time.Now().Add(delay)
	state.reason = reason
	r.entries[key] = state

	logger.Warnf("[EndpointCooldown] account=%q endpoint=%s reason=%q failures=%d cooldown=%s",
		endpointAccountLabel(account), ep.Name, reason, state.failures, delay.Round(time.Second))
}

func (r *endpointCooldownRegistry) recordSuccess(account *config.Account, ep kiroEndpoint) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.entries, endpointCooldownKey(account, ep))
}

func cooldownDelay(failures int, base, max time.Duration) time.Duration {
	if failures < 1 {
		failures = 1
	}
	delay := base
	for i := 1; i < failures; i++ {
		delay *= 2
		if delay >= max {
			return max
		}
	}
	if delay > max {
		return max
	}
	return delay
}

func endpointCooldownKey(account *config.Account, ep kiroEndpoint) string {
	return endpointAccountKey(account) + "|" + endpointIdentity(ep)
}

func endpointAccountKey(account *config.Account) string {
	if account == nil {
		return "<nil>"
	}
	if account.ID != "" {
		return account.ID
	}
	return account.Email
}

func endpointAccountLabel(account *config.Account) string {
	if account == nil {
		return "<nil>"
	}
	if account.Email != "" {
		return account.Email
	}
	return account.ID
}

func endpointIdentity(ep kiroEndpoint) string {
	if ep.Name != "" {
		return ep.Name
	}
	return ep.URL + "|" + ep.AmzTarget
}
