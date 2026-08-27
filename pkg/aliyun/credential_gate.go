package aliyun

import (
	"context"
	"sync"
)

type contextGate struct {
	once  sync.Once
	token chan struct{}
}

func (g *contextGate) Lock(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	g.once.Do(func() { g.token = make(chan struct{}, 1) })
	select {
	case g.token <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (g *contextGate) Unlock() {
	if g == nil || g.token == nil {
		return
	}
	<-g.token
}
