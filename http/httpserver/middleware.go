package httpserver

import (
	"cmp"
	"net/http"
	"slices"
)

// Middleware handles a request on its way to a handler. It calls next to pass the request on, or writes a response
// itself and returns to end it there. next is the rest of the chain after this middleware, built once when the chain
// is, so it's the same for every request and safe for concurrent use.
type Middleware interface {
	ServeHTTP(w http.ResponseWriter, r *http.Request, next http.Handler)
}

// OrderedMiddleware is a Middleware that declares where it runs in a chain. Lower orders run first, outermost; a
// Middleware that isn't an OrderedMiddleware counts as order 0. Middleware with equal orders run in an unspecified
// relative order.
type OrderedMiddleware interface {
	Middleware
	Order() int
}

// MiddlewareFunc adapts a function to a Middleware. A func(http.Handler) http.Handler, as most ecosystem middleware
// is written, adapts as
//
//	httpserver.MiddlewareFunc(func(w http.ResponseWriter, r *http.Request, next http.Handler) {
//		wrap(next).ServeHTTP(w, r)
//	})
//
// which calls wrap for every request, so wrap should do its setup before returning the function, not in it.
type MiddlewareFunc func(w http.ResponseWriter, r *http.Request, next http.Handler)

// ordered is the OrderedMiddleware Ordered returns.
type ordered struct {
	Middleware

	order int
}

// ServeHTTP calls f.
func (f MiddlewareFunc) ServeHTTP(w http.ResponseWriter, r *http.Request, next http.Handler) {
	f(w, r, next)
}

// Order returns the order Ordered was given.
func (o ordered) Order() int {
	return o.order
}

// Ordered returns m at order, replacing any order m declares itself, so an app can place middleware it didn't write.
func Ordered(order int, m Middleware) OrderedMiddleware {
	if o, ok := m.(ordered); ok {
		m = o.Middleware
	}

	return ordered{Middleware: m, order: order}
}

// Chain returns h wrapped in mws, lowest order outermost, with h innermost. Middleware with equal orders keep their
// relative order in mws. Each middleware's next is built here, once. Chain with no middleware returns h itself.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	sorted := slices.Clone(mws)

	slices.SortStableFunc(sorted, func(a, b Middleware) int {
		return cmp.Compare(orderOf(a), orderOf(b))
	})

	for _, m := range slices.Backward(sorted) {
		h = &link{middleware: m, next: h}
	}

	return h
}

// link is one position in a chain: a middleware and the rest of the chain after it.
type link struct {
	middleware Middleware
	next       http.Handler
}

// ServeHTTP passes the request to the middleware, with the rest of the chain as its next.
func (l *link) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	l.middleware.ServeHTTP(w, r, l.next)
}

// orderOf returns m's declared order, or 0 when it declares none.
func orderOf(m Middleware) int {
	if o, ok := m.(OrderedMiddleware); ok {
		return o.Order()
	}

	return 0
}
