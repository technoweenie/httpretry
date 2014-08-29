package httpretry

import (
	"time"

	"github.com/cenkalti/backoff"
)

var DefaultBackOff = func() backoff.BackOff {
	return backoff.NewExponentialBackOff()
}

type QuittableBackOff struct {
	b      backoff.BackOff
	IsDone bool
}

func (b *QuittableBackOff) Done() {
	b.IsDone = true
}

func (b *QuittableBackOff) Reset() {
	b.IsDone = false
	b.b.Reset()
}

func (b *QuittableBackOff) NextBackOff() time.Duration {
	if b.IsDone == true {
		return backoff.Stop
	}
	return b.b.NextBackOff()
}
