package proxy

import (
	"kiro-go/config"
	"kiro-go/logger"
	"time"
)

var (
	accountQueueMaxWait = 15 * time.Second
	accountQueuePoll    = 200 * time.Millisecond
)

func (h *Handler) selectAccountForModel(model string, excluded map[string]bool) *config.Account {
	deadline := time.Now().Add(accountQueueMaxWait)
	waitLogged := false

	for {
		account := h.pool.GetNextForModelExcluding(model, excluded)
		if account == nil {
			return nil
		}

		if remaining := h.pool.CooldownRemaining(account.ID); remaining > 0 {
			logger.Warnf("[AccountQueue] selected account=%q is cooling down for %s; no ready account available", account.Email, remaining.Round(time.Second))
			return nil
		}

		inFlight := h.pool.InFlightCount(account.ID)
		if inFlight <= 0 {
			return account
		}

		if accountQueueMaxWait <= 0 || time.Now().After(deadline) {
			logger.Warnf("[AccountQueue] wait exhausted; using busy account=%q inFlight=%d", account.Email, inFlight)
			return account
		}

		if !waitLogged {
			logger.Infof("[AccountQueue] all candidate accounts busy; waiting up to %s before adding concurrency", accountQueueMaxWait.Round(time.Second))
			waitLogged = true
		}

		sleepFor := accountQueuePoll
		if remaining := time.Until(deadline); remaining < sleepFor {
			sleepFor = remaining
		}
		if sleepFor > 0 {
			time.Sleep(sleepFor)
		}
	}
}
