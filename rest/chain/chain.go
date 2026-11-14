package chain

import "net/http"

// Middleware wraps an HTTP handler.
type Middleware func(http.HandlerFunc) http.HandlerFunc

// Chain is an immutable sequence of middleware.
type Chain struct {
	middlewares []Middleware
}

// New creates a middleware chain.
func New(middlewares ...Middleware) Chain {
	return Chain{middlewares: append([]Middleware(nil), middlewares...)}
}

// Append returns a chain with middlewares added to the end of the request flow.
func (c Chain) Append(middlewares ...Middleware) Chain {
	all := make([]Middleware, 0, len(c.middlewares)+len(middlewares))
	all = append(all, c.middlewares...)
	all = append(all, middlewares...)
	return Chain{middlewares: all}
}

// Prepend returns a chain with middlewares added to the beginning of the request flow.
func (c Chain) Prepend(middlewares ...Middleware) Chain {
	all := make([]Middleware, 0, len(c.middlewares)+len(middlewares))
	all = append(all, middlewares...)
	all = append(all, c.middlewares...)
	return Chain{middlewares: all}
}

// Then applies the chain to h. The first middleware runs first.
func (c Chain) Then(h http.Handler) http.Handler {
	if h == nil {
		h = http.DefaultServeMux
	}

	next := http.HandlerFunc(h.ServeHTTP)
	for i := len(c.middlewares) - 1; i >= 0; i-- {
		// Skip nil entries so a conditional middleware (or Use(nil)) degrades to
		// a no-op instead of panicking on the first request.
		if c.middlewares[i] == nil {
			continue
		}
		next = c.middlewares[i](next)
	}
	return next
}

// ThenFunc applies the chain to a handler function.
func (c Chain) ThenFunc(h http.HandlerFunc) http.Handler {
	if h == nil {
		return c.Then(nil)
	}
	return c.Then(h)
}
