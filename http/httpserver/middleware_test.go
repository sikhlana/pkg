package httpserver_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sikhlana/pkg/http/httpserver"
)

// trail records the order middleware and the handler see a request in.
type trail struct {
	mu    sync.Mutex
	steps []string
}

func (tr *trail) add(step string) {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	tr.steps = append(tr.steps, step)
}

func (tr *trail) all() []string {
	tr.mu.Lock()
	defer tr.mu.Unlock()

	return append([]string(nil), tr.steps...)
}

// step is a middleware that records its label before passing the request on.
type step struct {
	trail *trail
	label string
}

func (s step) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.Handler) {
	s.trail.add(s.label)
	next.ServeHTTP(w, r)
}

// orderedStep is a step that declares an order.
type orderedStep struct {
	step

	order int
}

func (s orderedStep) Order() int {
	return s.order
}

// handler returns a handler that records "handler".
func (tr *trail) handler() http.Handler {
	return http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		tr.add("handler")
	})
}

// serve sends one request through h and returns the response recorder.
func serve(h http.Handler) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/", nil))

	return w
}

func TestChainRunsLowerOrdersOuter(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	serve(httpserver.Chain(tr.handler(),
		orderedStep{step{tr, "200"}, 200},
		orderedStep{step{tr, "10"}, 10},
		orderedStep{step{tr, "100"}, 100},
	))

	assert.Equal(t, []string{"10", "100", "200", "handler"}, tr.all())
}

func TestChainCountsAnUndeclaredOrderAsZero(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	serve(httpserver.Chain(tr.handler(),
		orderedStep{step{tr, "5"}, 5},
		step{tr, "undeclared"},
		orderedStep{step{tr, "-5"}, -5},
	))

	assert.Equal(t, []string{"-5", "undeclared", "5", "handler"}, tr.all())
}

func TestOrderedOverridesADeclaredOrder(t *testing.T) {
	t.Parallel()

	tr := &trail{}
	wrapped := httpserver.Ordered(1, orderedStep{step{tr, "declared 100"}, 100})

	serve(httpserver.Chain(tr.handler(), orderedStep{step{tr, "50"}, 50}, wrapped))

	assert.Equal(t, 1, wrapped.Order())
	assert.Equal(t, []string{"declared 100", "50", "handler"}, tr.all())
}

func TestOrderedTwiceKeepsTheLastOrder(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	assert.Equal(t, 7, httpserver.Ordered(7, httpserver.Ordered(3, step{tr, "x"})).Order())
}

func TestChainRunsEqualOrdersInTheirGivenOrder(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	serve(httpserver.Chain(tr.handler(), step{tr, "a"}, step{tr, "b"}, orderedStep{step{tr, "c"}, 0}))

	assert.Equal(t, []string{"a", "b", "c", "handler"}, tr.all())
}

func TestChainWithoutMiddlewareReturnsTheHandler(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	serve(httpserver.Chain(tr.handler()))

	assert.Equal(t, []string{"handler"}, tr.all())
}

func TestChainShortCircuits(t *testing.T) {
	t.Parallel()

	tr := &trail{}
	deny := httpserver.MiddlewareFunc(func(w http.ResponseWriter, _ *http.Request, _ http.Handler) {
		tr.add("deny")
		w.WriteHeader(http.StatusUnauthorized)
	})

	w := serve(httpserver.Chain(tr.handler(), httpserver.Ordered(-1, deny), step{tr, "later"}))

	assert.Equal(t, http.StatusUnauthorized, w.Code)
	assert.Equal(t, []string{"deny"}, tr.all())
}

func TestChainBuildsEachNextOnce(t *testing.T) {
	t.Parallel()

	var (
		mu   sync.Mutex
		seen []http.Handler
	)

	record := httpserver.MiddlewareFunc(func(w http.ResponseWriter, r *http.Request, next http.Handler) {
		mu.Lock()

		seen = append(seen, next)
		mu.Unlock()

		next.ServeHTTP(w, r)
	})
	inner := httpserver.Ordered(1, httpserver.MiddlewareFunc(
		func(w http.ResponseWriter, r *http.Request, next http.Handler) { next.ServeHTTP(w, r) },
	))

	h := httpserver.Chain(http.NotFoundHandler(), record, inner)

	var wg sync.WaitGroup

	for range 50 {
		wg.Go(func() { serve(h) })
	}

	wg.Wait()

	require.Len(t, seen, 50)

	for _, next := range seen[1:] {
		assert.Same(t, seen[0], next)
	}
}

// pointerStep is a step with a pointer receiver, so a nil *pointerStep is a typed nil behind Middleware.
type pointerStep struct {
	step
}

func (s *pointerStep) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.Handler) {
	s.step.ServeHTTP(w, r, next)
}

func TestChainLeavesOutAbsentMiddleware(t *testing.T) {
	t.Parallel()

	tests := map[string]httpserver.Middleware{
		"nil":                    nil,
		"nil pointer":            (*pointerStep)(nil),
		"nil func":               httpserver.MiddlewareFunc(nil),
		"Ordered nil":            httpserver.Ordered(10, nil),
		"Ordered nil pointer":    httpserver.Ordered(10, (*pointerStep)(nil)),
		"Ordered twice nil func": httpserver.Ordered(5, httpserver.Ordered(10, httpserver.MiddlewareFunc(nil))),
	}

	for name, absent := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			tr := &trail{}

			w := serve(httpserver.Chain(tr.handler(), step{tr, "kept"}, absent, &pointerStep{step{tr, "pointer"}}))

			assert.Equal(t, http.StatusOK, w.Code)
			assert.Equal(t, []string{"kept", "pointer", "handler"}, tr.all())
		})
	}
}

func TestChainOfOnlyAbsentMiddlewareReturnsTheHandler(t *testing.T) {
	t.Parallel()

	handler := &pointerHandler{}

	assert.Same(t, handler, httpserver.Chain(handler, nil, (*pointerStep)(nil)))
}

// pointerHandler is an http.Handler with an identity, for checking Chain returns it unwrapped.
type pointerHandler struct{}

func (*pointerHandler) ServeHTTP(http.ResponseWriter, *http.Request) {}

func TestMiddlewareUnderFx(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	var calls atomic.Int32

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain(), "admin": plain()},
			provide("api", &calls),
			provide("admin", &calls),
			httpserver.ProvideMiddleware("api", func() orderedStep { return orderedStep{step{tr, "200"}, 200} }),
			httpserver.ProvideMiddleware("api", func() orderedStep { return orderedStep{step{tr, "10"}, 10} }),
			httpserver.ProvideMiddleware("api", func() httpserver.OrderedMiddleware {
				return httpserver.Ordered(1, orderedStep{step{tr, "100 wrapped as 1"}, 100})
			}),
			httpserver.ProvideMiddleware("api", func() step { return step{tr, "undeclared"} }),
			httpserver.ProvideMiddleware("api", func() step { return step{tr, "undeclared"} }),
			httpserver.ProvideMiddleware("admin", func() step { return step{tr, "admin only"} }),
		),
		httpserver.ProvideRunner("api"),
		httpserver.ProvideRunner("admin"),
		probe(func(ctx context.Context, s servers) error {
			_, _, err := fetch(ctx, s.API.Addr())

			return err
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"undeclared", "undeclared", "100 wrapped as 1", "10", "200"}, tr.all(), "not admin only")
}

func TestConditionalMiddlewareIsLeftOut(t *testing.T) {
	t.Parallel()

	tr := &trail{}

	var calls atomic.Int32

	// Each constructor decides from a condition known only at build time, as one reading the app's config would.
	conditional := func(enabled bool, label string) func() httpserver.Middleware {
		return func() httpserver.Middleware {
			if !enabled {
				return nil
			}

			return step{tr, label}
		}
	}

	err := run(t,
		shared(slog.New(slog.DiscardHandler), configs{"api": plain()},
			provide("api", &calls),
			httpserver.ProvideMiddleware("api", conditional(true, "enabled")),
			httpserver.ProvideMiddleware("api", conditional(false, "disabled")),
			httpserver.ProvideMiddleware("api", func() *pointerStep { return nil }),
		),
		httpserver.ProvideRunner("api"),
		probe(func(ctx context.Context, s servers) error {
			status, _, err := fetch(ctx, s.API.Addr())
			if err != nil {
				return err
			}

			assert.Equal(t, http.StatusOK, status)

			return nil
		}),
	)

	require.NoError(t, err)
	assert.Equal(t, []string{"enabled"}, tr.all())
}
