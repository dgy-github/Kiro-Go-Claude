package proxy

import (
	"kiro-go/config"
	"testing"
)

func TestAccountFailureClassifiers(t *testing.T) {
	tests := []struct {
		name string
		fn   func(string) bool
		msg  string
	}{
		{name: "quota", fn: isQuotaErrorMessage, msg: "HTTP 429: quota exhausted"},
		{name: "overage", fn: isOverageErrorMessage, msg: "HTTP 402 from Kiro IDE: OVERAGE limit exceeded"},
		{name: "suspension", fn: isSuspensionErrorMessage, msg: "Your User ID temporarily is suspended"},
		{name: "profile", fn: isProfileUnavailableErrorMessage, msg: "no available Kiro profile"},
		{name: "auth", fn: isAuthErrorMessage, msg: "Authentication failed - token invalid or expired"},
	}

	for _, tc := range tests {
		if !tc.fn(tc.msg) {
			t.Fatalf("%s classifier did not match %q", tc.name, tc.msg)
		}
	}
}

func TestAccountUsageAtLimitOnlyWhenSubscriptionUsageExhausted(t *testing.T) {
	if isAccountUsageAtLimit(&config.Account{UsageCurrent: 822, UsageLimit: 10000}) {
		t.Fatalf("expected low-usage account to avoid hard quota cooldown")
	}
	if !isAccountUsageAtLimit(&config.Account{UsageCurrent: 10000, UsageLimit: 10000}) {
		t.Fatalf("expected exhausted account to be hard quota-limited")
	}
	if isAccountUsageAtLimit(&config.Account{}) {
		t.Fatalf("expected missing usage limit to avoid hard quota cooldown")
	}
}
