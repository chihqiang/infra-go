package trace

import (
	"os"
	"sync"

	"github.com/chihqiang/infra-go/mapping"
	"go.opentelemetry.io/otel/attribute"
)

// fillDefaultUnmarshaler is the unmarshaler used to fill in default values.
var fillDefaultUnmarshaler = mapping.NewDefaultUnmarshaler()

// fillDefault fills in the defaults, then overrides them with the non-zero fields of
// the user config and finally applies opts.
//
// Limitation: the non-zero-field override rule cannot tell "unset" apart from
// "explicitly set to 0"; to express an explicit 0 (such as Sampler = 0) pass it through
// opts, see Option.
func fillDefault(cfg Config, opts ...Option) Config {
	var c Config
	if err := fillDefaultUnmarshaler.Unmarshal(map[string]any{}, &c); err != nil {
		panic(err)
	}

	// Override the defaults with the non-zero fields of the user config
	if cfg.Name != "" {
		c.Name = cfg.Name
	}
	// Endpoint: the empty string is a valid value too
	c.Endpoint = cfg.Endpoint
	if cfg.Sampler > 0 {
		c.Sampler = cfg.Sampler
	}
	if cfg.Batcher != "" {
		c.Batcher = cfg.Batcher
	}
	if len(cfg.OtlpHeaders) > 0 {
		c.OtlpHeaders = cfg.OtlpHeaders
	}
	if cfg.OtlpHttpPath != "" {
		c.OtlpHttpPath = cfg.OtlpHttpPath
	}
	if cfg.OtlpHttpSecure {
		c.OtlpHttpSecure = cfg.OtlpHttpSecure
	}
	if cfg.OtlpGrpcSecure {
		c.OtlpGrpcSecure = cfg.OtlpGrpcSecure
	}
	if cfg.Disabled {
		c.Disabled = cfg.Disabled
	}

	// Options are applied last: they can override the "zero means unset" decision above
	for _, opt := range opts {
		opt(&c)
	}

	return c
}

// --- Resource management ---

var (
	// attrResources is attached to the Resource of every span.
	//
	// Guarded by a lock: AddResources may be called by business code at runtime
	// (dynamic tagging) while startAgent reads it to build the Resource; without the
	// lock -race detects a data race.
	attrResourcesLk sync.RWMutex
	attrResources   = make([]attribute.KeyValue, 0)
)

// AddResources adds extra resource attributes.
// Resource attributes are attached to every span and identify the service origin.
// Create attributes with the AttrString / AttrInt helpers, no need to import
// otel/attribute.
//
// Concurrency-safe. Note: attributes are snapshotted when the TracerProvider is
// created, so **attributes added after startup have no effect** (a new StartAgent is
// required).
func AddResources(attrs ...Attr) {
	if len(attrs) == 0 {
		return
	}
	attrResourcesLk.Lock()
	attrResources = append(attrResources, attrs...)
	attrResourcesLk.Unlock()
}

// resourceAttrs returns a copy of the resource attributes, used to build a Resource.
func resourceAttrs() []attribute.KeyValue {
	attrResourcesLk.RLock()
	defer attrResourcesLk.RUnlock()
	out := make([]attribute.KeyValue, len(attrResources))
	copy(out, attrResources)
	return out
}

// openFileForExporter opens a file for the file exporter.
// It returns the file and its close function: the caller is responsible for closing it
// when the agent stops, instead of keeping the closer in a package-level variable
// (multiple instances overwrite each other and StopAgent could not release it).
func openFileForExporter(path string) (*os.File, func() error, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, nil, err
	}
	return f, f.Close, nil
}
