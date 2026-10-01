package protection

import (
	"context"
	"strings"
	"sync/atomic"
)

type admissionContextKey struct{}
type admittedRequest struct {
	limiter   *Limiter
	operation Operation
	account   string
	used      atomic.Uint32
}

// AdmittedBudget bounds credential work after durable transport admission.
// Signup/signin/reset allow one operation; password change allows current
// verification then replacement hashing once each. This is a resource budget,
// not proof of successful credential verification or authorization.
type AdmittedBudget struct{ limiter *Limiter }

func (l *Limiter) PasswordBudget() *AdmittedBudget { return &AdmittedBudget{limiter: l} }
func (b *AdmittedBudget) Allow(ctx context.Context, key string) error {
	if b == nil || b.limiter == nil || ctx == nil || ctx.Err() != nil {
		return ErrUnavailable
	}
	proof, ok := ctx.Value(admissionContextKey{}).(*admittedRequest)
	if !ok || proof == nil || proof.limiter != b.limiter {
		return ErrUnavailable
	}
	var expected string
	var before, after uint32
	switch proof.operation {
	case Signup:
		expected = "register:" + proof.account
		after = 1
	case Signin:
		expected = "signin:" + proof.account
		after = 1
	case Recovery:
		expected = "reset:" + proof.account
		after = 1
	case PasswordChange:
		switch key {
		case "change-current:" + proof.account:
			expected = key
			after = 1
		case "change-new:" + proof.account:
			expected = key
			before = 1
			after = 3
		default:
			return ErrUnavailable
		}
	default:
		return ErrUnavailable
	}
	if key != expected || !proof.used.CompareAndSwap(before, after) {
		return ErrUnavailable
	}
	return nil
}
func (g Guard) admittedContext(rctx context.Context, op Operation, account string) context.Context {
	account = strings.ToLower(strings.TrimSpace(account))
	if account == "" {
		account = "invalid-input"
	}
	return context.WithValue(rctx, admissionContextKey{}, &admittedRequest{limiter: g.Limiter, operation: op, account: account})
}
