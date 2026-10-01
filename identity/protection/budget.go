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
	used      atomic.Bool
}

// AdmittedBudget grants one expensive credential operation only after durable
// transport admission. Its private proof cannot be constructed by extensions.
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
	var prefix string
	switch proof.operation {
	case Signup:
		prefix = "register:"
	case Signin:
		prefix = "signin:"
	default:
		return ErrUnavailable
	}
	if key != prefix+proof.account || !proof.used.CompareAndSwap(false, true) {
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
