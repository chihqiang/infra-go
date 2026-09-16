package service

import (
	"fmt"
	"sync"

	"github.com/chihqiang/infra-go/logger"
)

// --- Interface definitions ---

// Starter wraps the Start method and is used to start a service.
type Starter interface {
	Start()
}

// Stopper wraps the Stop method and is used to stop a service.
type Stopper interface {
	Stop()
}

// Service is the interface of a service that can both start and stop.
//
// Implementation contract (important):
//   - Start usually blocks until the service exits (for example while waiting for a stop
//     signal).
//   - Stop may be called at any moment, including concurrently with Start or right after
//     Start has begun. The implementation must be idempotent and must tolerate a "stop
//     request arriving before the service has finished its own initialisation" (for example
//     by recording the stop intent first and performing the real shutdown once
//     initialisation completes).
//
// ServiceGroup guarantees that Stop is issued at most once and never before that service's
// Start has been called (see the start barrier in ServiceGroup.doStop); it cannot guarantee
// that Stop comes after the initialisation inside Start has finished, and that window is up to
// each service to handle.
type Service interface {
	Starter
	Stopper
}

// --- ServiceGroup ---

// ServiceGroup manages a group of Services and supports concurrent start and stop.
//
// Start: every Service is started concurrently and ServiceGroup.Start blocks until all of them
// return.
// Stop: every Service is stopped concurrently and Stop runs only once (sync.Once).
// Order: Add appends to the tail (O(1)), starting is concurrent in the order of addition and
// stopping is concurrent in the reverse order of addition.
// Panic: panics in Start and Stop are recovered and logged through logger without interrupting
// the other services. A panic in Start automatically triggers Stop to unblock the other
// services.
//
// Lifecycle constraint: Add must be called before Start. Services added after Start has begun
// are neither started nor stopped (Start/Stop take a snapshot of the service list when they
// dispatch).
//
// Typical usage:
//
//	sg := service.NewServiceGroup()
//	sg.Add(httpService)
//	sg.Add(redisService)
//	sg.Start() // blocks and returns once every service has exited
type ServiceGroup struct {
	mu       sync.Mutex
	services []Service
	// started marks that doStart has begun, which lets doStop decide whether it has to wait
	// for the start barrier.
	started bool
	// allEntered is closed before every service goroutine enters Start, acting as the stop
	// barrier.
	allEntered chan struct{}
	stopOnce   func()
}

// NewServiceGroup creates a ServiceGroup.
func NewServiceGroup() *ServiceGroup {
	sg := &ServiceGroup{
		allEntered: make(chan struct{}),
	}
	sg.stopOnce = sync.OnceFunc(sg.doStop)
	return sg
}

// Add adds service to the group.
// It appends to the tail (O(1)); starting follows the order of addition and stopping follows
// the reverse order (the most recently added stops first).
//
// It must be called before Start: Start takes a snapshot of the service list, so services
// added afterwards are neither started nor stopped.
// Concurrent calls are safe, but the order between a concurrent Add and Start is undetermined.
func (sg *ServiceGroup) Add(service Service) {
	sg.mu.Lock()
	sg.services = append(sg.services, service)
	sg.mu.Unlock()
}

// snapshot returns a copy of the service list, avoiding running user code (Start/Stop) while
// holding the lock.
func (sg *ServiceGroup) snapshot() []Service {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	// Copy it: even if the caller adds a service concurrently during Start, the slice being
	// iterated is never modified.
	out := make([]Service, len(sg.services))
	copy(out, sg.services)
	return out
}

// Start starts every Service concurrently and blocks until all of them have exited.
// If a Service panics inside Start, Stop is triggered automatically to stop the other
// services and the error is recorded through logger (including the service index and type
// name); the panic is not re-raised.
//
// Stop barrier: doStop waits until every service goroutine has entered Start before
// dispatching Stop, which avoids a "service stopped before it ever started and then never
// receiving the stop signal once it does start".
// No further logic should follow a call to this method.
func (sg *ServiceGroup) Start() {
	sg.doStart()
}

// Stop stops every Service concurrently and runs only once.
func (sg *ServiceGroup) Stop() {
	sg.stopOnce()
}

// doStart starts every Service concurrently and waits for all of them to exit.
// A panic in Start is recovered, triggers Stop to unblock the other services and is recorded
// through logger.
func (sg *ServiceGroup) doStart() {
	services := sg.snapshot()

	// Mark that starting has begun: doStop uses this to decide whether it has to wait for the
	// start barrier.
	// It must be set before the goroutines are started, otherwise Stop could arrive before the
	// mark and skip the wait.
	sg.mu.Lock()
	sg.started = true
	sg.mu.Unlock()

	// Start barrier counter: every service calls Done once before entering Start. Once all of
	// them have entered Start, closing allEntered lets doStop dispatch the stop.
	var entered sync.WaitGroup
	entered.Add(len(services))
	go func() {
		entered.Wait()
		close(sg.allEntered)
	}()

	var wg sync.WaitGroup
	var panicOnce sync.Once

	for i, svc := range services {
		wg.Add(1)
		go func(idx int, s Service) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicOnce.Do(func() {
						logger.Errorf("service: panic during start, index: %d, type: %T, reason: %v",
							idx, s, r)
						// Trigger the stop synchronously to make sure the blocked Start calls of
						// the other services are released.
						// doStop first waits until every service has entered Start, so a
						// "Stop before Start" that would leave a service without a stop signal
						// forever cannot happen.
						sg.stopOnce()
					})
				}
			}()
			// Report before entering Start, so that the stop barrier can be released even while
			// Start blocks.
			entered.Done()
			s.Start()
		}(i, svc)
	}
	wg.Wait()
}

// doStop stops every Service concurrently and waits for all of them to finish.
// A panic in Stop is only logged and does not interrupt the stopping of the other services.
func (sg *ServiceGroup) doStop() {
	// Wait until every service has entered Start before dispatching the stop.
	//
	// Without the wait, a Stop triggered by a panicking service would act on services that
	// have not started yet: their Stop is called first (usually a no-op) and only afterwards
	// does Start run, while stopOnce is already exhausted, so those services never receive a
	// stop signal once they start (they block inside Start and the process cannot exit).
	//
	// Only wait when Start has already begun: otherwise allEntered is never closed and Stop
	// would block forever.
	sg.mu.Lock()
	started := sg.started
	sg.mu.Unlock()
	if started {
		<-sg.allEntered
	}

	var wg sync.WaitGroup
	// Iterate in reverse: the most recently added service stops first
	services := sg.snapshot()
	for i := len(services) - 1; i >= 0; i-- {
		svc := services[i]
		wg.Add(1)
		go func(idx int, s Service) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("service: panic during stop, index: %d, type: %T, reason: %v",
						idx, s, r)
				}
			}()
			s.Stop()
		}(i, svc)
	}
	wg.Wait()
}

// --- Adapter functions ---

// WithStart wraps a start func as a Service (Stop is a no-op).
func WithStart(start func()) Service {
	return startOnlyService{start: start}
}

// WithStarter wraps a Starter as a Service (Stop is a no-op).
func WithStarter(start Starter) Service {
	return starterOnlyService{Starter: start}
}

// --- Internal adapter types ---

// noopStopper is a Stopper implementation whose Stop is a no-op.
type noopStopper struct{}

func (noopStopper) Stop() {}

type startOnlyService struct {
	start func()
	noopStopper
}

func (s startOnlyService) Start() {
	s.start()
}

type starterOnlyService struct {
	Starter
	noopStopper
}

// --- Adapter for services that return errors ---

// AsService adapts an object implementing Start() error and Stop() error into a Service, so
// that it can be added to a ServiceGroup directly.
// A typical object is *httpx.Server (both Start and Stop return an error, a signature that
// differs from Service.Starter/Stopper).
//
//	sg := service.NewServiceGroup()
//	sg.Add(service.AsService(srv)) // srv *httpx.Server
//	sg.Start()
//
// The errors returned by Start / Stop are logged as errors (the Service interface has no
// return value, so they cannot be propagated upwards); a panic in Start is still recovered by
// ServiceGroup, which also triggers Stop.
func AsService[T interface {
	Start() error
	Stop() error
}](s T) Service {
	return errorServiceAdapter[T]{s: s}
}

// errorServiceAdapter wraps an object with Start() error + Stop() error as a Service that
// returns nothing.
type errorServiceAdapter[T interface {
	Start() error
	Stop() error
}] struct {
	s T
}

func (a errorServiceAdapter[T]) Start() {
	if err := a.s.Start(); err != nil {
		logger.Error("service: managed service start returned error",
			logger.Err(err),
			logger.String("type", fmt.Sprintf("%T", a.s)))
	}
}

func (a errorServiceAdapter[T]) Stop() {
	if err := a.s.Stop(); err != nil {
		logger.Error("service: managed service stop returned error",
			logger.Err(err),
			logger.String("type", fmt.Sprintf("%T", a.s)))
	}
}
