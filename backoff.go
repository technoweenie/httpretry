package httpretry

import (
	"time"

	"github.com/cenkalti/backoff"
)

// DefaultBackOff returns a new backoff.BackOff that's used by default for ever
// new *HttpGetter.
var DefaultBackOff = func() backoff.BackOff {
	return backoff.NewExponentialBackOff()
}

// QuittableBackOff is a backoff.BackOff that halts future retries after Done()
// gets called.
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
