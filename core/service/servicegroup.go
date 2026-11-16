package service

import (
	"errors"
	"fmt"
	"net/http"
	"sync"

	"github.com/chihqiang/infra-go/core/logger"
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
	// startDone is closed when the first Start returns; later Start calls wait on it so
	// they observe the same lifetime instead of closing allEntered twice.
	startDone chan struct{}
	// stopRequested records that Stop arrived before Start. Some services implement Stop
	// as a no-op until initialization has run, so the group issues Stop again once every
	// service has entered Start.
	stopRequested bool
	stopOnce      func()
}

// NewServiceGroup creates a ServiceGroup.
func NewServiceGroup() *ServiceGroup {
	sg := &ServiceGroup{
		allEntered: make(chan struct{}),
		startDone:  make(chan struct{}),
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
// Start is safe to call more than once: the first call runs the group and later calls
// wait for it to return instead of starting the services again (which used to close the
// start barrier twice and panic with "close of closed channel").
//
// Stop barrier: doStop waits until every service goroutine has entered Start before
// dispatching Stop, which avoids a "service stopped before it ever started and then never
// receiving the stop signal once it does start". When Stop arrived before Start, the
// group issues Stop a second time after the barrier, because a Stop before initialization
// may have been a no-op.
func (sg *ServiceGroup) Start() {
	sg.mu.Lock()
	if sg.started {
		done := sg.startDone
		sg.mu.Unlock()
		<-done
		return
	}
	sg.started = true
	sg.mu.Unlock()

	defer close(sg.startDone)
	sg.doStart()
}

// Stop stops every Service and runs only once. When it is called before Start, the
// request is recorded and Stop is issued again after the services have started, so a
// service whose Stop is a no-op before initialization still receives the signal.
func (sg *ServiceGroup) Stop() {
	sg.mu.Lock()
	if !sg.started {
		sg.stopRequested = true
	}
	sg.mu.Unlock()
	sg.stopOnce()
}

// doStart starts every Service concurrently and waits for all of them to exit.
// A panic in Start is recovered, triggers Stop to unblock the other services and is recorded
// through logger.
func (sg *ServiceGroup) doStart() {
	services := sg.snapshot()

	// Start barrier counter: every service calls Done once before entering Start. Once all of
	// them have entered Start, closing allEntered lets doStop dispatch the stop.
	var entered sync.WaitGroup
	entered.Add(len(services))
	go func() {
		entered.Wait()
		close(sg.allEntered)

		// Stop before Start: the first Stop call ran before this barrier was released,
		// so services whose Stop is a no-op until initialized never got the signal.
		// Issue it again now that every Start has been entered.
		sg.mu.Lock()
		requested := sg.stopRequested
		sg.mu.Unlock()
		if requested {
			sg.stopAll()
		}
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

// doStop waits for the start barrier and then stops every Service.
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
	sg.stopAll()
}

// stopAll stops every Service one at a time in reverse registration order, so the
// most recently added service (usually the traffic entry point) stops first and
// dependencies it uses are still alive. A slow Stop delays the services before it;
// that is the price of the ordering guarantee.
func (sg *ServiceGroup) stopAll() {
	services := sg.snapshot()
	for i := len(services) - 1; i >= 0; i-- {
		svc := services[i]
		func(idx int, s Service) {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("service: panic during stop, index: %d, type: %T, reason: %v",
						idx, s, r)
				}
			}()
			s.Stop()
		}(i, svc)
	}
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
// A typical object is *rest.Server (both Start and Stop return an error, a signature that
// differs from Service.Starter/Stopper).
//
//	sg := service.NewServiceGroup()
//	sg.Add(service.AsService(srv)) // srv *rest.Server
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
	err := a.s.Start()
	// http.Server.Serve returns ErrServerClosed after Shutdown/Close; that is a
	// normal graceful stop, not an error worth logging.
	if err != nil && !errors.Is(err, http.ErrServerClosed) {
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
