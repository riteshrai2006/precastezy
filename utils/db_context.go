package utils

import (
	"context"
	"time"
)

// DefaultQueryTimeout is the default timeout for database queries
const DefaultQueryTimeout = 30 * time.Second

// FastQueryTimeout is for simple queries that should be fast
const FastQueryTimeout = 10 * time.Second

// SlowQueryTimeout is for complex queries that may take longer
const SlowQueryTimeout = 60 * time.Second

// GetQueryContext returns a context with timeout for database queries
// If parent context is provided, it uses that; otherwise creates a background context
func GetQueryContext(parentCtx context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return context.WithTimeout(parentCtx, timeout)
}

// GetDefaultQueryContext returns a context with default timeout
func GetDefaultQueryContext(parentCtx context.Context) (context.Context, context.CancelFunc) {
	return GetQueryContext(parentCtx, DefaultQueryTimeout)
}

// GetFastQueryContext returns a context with fast query timeout
func GetFastQueryContext(parentCtx context.Context) (context.Context, context.CancelFunc) {
	return GetQueryContext(parentCtx, FastQueryTimeout)
}

// GetSlowQueryContext returns a context with slow query timeout
func GetSlowQueryContext(parentCtx context.Context) (context.Context, context.CancelFunc) {
	return GetQueryContext(parentCtx, SlowQueryTimeout)
}
