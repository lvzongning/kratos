package kratos

import (
	"context"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

// Lifecycle is component lifecycle.
type Lifecycle interface {
	Start(context.Context) error
	Stop(context.Context) error
}

// Hook is a pair of start and stop callbacks.
type Hook struct {
	OnStart func(context.Context) error
	OnStop  func(context.Context) error
}

// App is an application components lifecycle manager
type App struct {
	opts  options
	hooks []Hook

	cancel func()
}

// New create an application lifecycle manager.
func New(opts ...Option) *App {
	options := options{
		id:           os.Getenv("KRATOS_SERVICE_ID"),
		name:         os.Getenv("KRATOS_SERVICE_NAME"),
		version:      os.Getenv("KRATOS_SERVICE_VERSION"),
		endpoints:    strings.Split(os.Getenv("KRATOS_SERVICE_ENDPOINTS"), ","),
		startTimeout: time.Second * 30,
		stopTimeout:  time.Second * 30,
		sigs: []os.Signal{
			syscall.SIGTERM,
			syscall.SIGQUIT,
			syscall.SIGINT,
		},
		sigFn: func(a *App, sig os.Signal) {
			switch sig {
			case syscall.SIGINT, syscall.SIGQUIT, syscall.SIGTERM:
				a.Stop()
			default:
			}
		},
	}
	for _, o := range opts {
		o(&options)
	}
	return &App{
		opts: options,
	}
}

// Append register interface that are executed on application start and stop.
func (a *App) Append(lc Lifecycle) {
	a.hooks = append(a.hooks, Hook{
		OnStart: func(ctx context.Context) error {
			return lc.Start(ctx)
		},
		OnStop: func(ctx context.Context) error {
			return lc.Stop(ctx)
		},
	})
}

// AppendHook register callbacks that are executed on application start and stop.
func (a *App) AppendHook(hook Hook) {
	a.hooks = append(a.hooks, hook)
}

// Run executes all OnStart hooks registered with the application's Lifecycle.
func (a *App) Run() error {
	var ctx context.Context
	ctx, a.cancel = context.WithCancel(context.Background())
	g, ctx := errgroup.WithContext(ctx)
	for _, hook := range a.hooks {
		hook := hook
		if hook.OnStop != nil {
			g.Go(func() error {
				<-ctx.Done() // wait for stop signal
				stopCtx, cancel := context.WithTimeout(context.Background(), a.opts.stopTimeout)
				defer cancel()
				return hook.OnStop(stopCtx)
			})
		}
		if hook.OnStart != nil {
			g.Go(func() error {
				startCtx, cancel := context.WithTimeout(context.Background(), a.opts.startTimeout)
				defer cancel()
				return hook.OnStart(startCtx)
			})
		}
	}
	if len(a.opts.sigs) == 0 {
		return g.Wait()
	}
	c := make(chan os.Signal, len(a.opts.sigs))
	signal.Notify(c, a.opts.sigs...)
	g.Go(func() error {
		for {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case sig := <-c:
				if a.opts.sigFn != nil {
					a.opts.sigFn(a, sig)
				}
			}
		}
	})
	return g.Wait()
}

// Stop gracefully stops the application.
func (a *App) Stop() {
	if a.cancel != nil {
		a.cancel()
	}
}
