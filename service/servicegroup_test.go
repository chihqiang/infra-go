package service

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- Service used by the tests ---

// mockService simulates a service: Start blocks until Stop is called.
type mockService struct {
	startCalled int32
	stopCalled  int32
	stopCh      chan struct{}
	stopOnce    sync.Once
	stopDelay   time.Duration
}

func newMockService() *mockService {
	return &mockService{
		stopCh: make(chan struct{}),
	}
}

func (m *mockService) Start() {
	atomic.StoreInt32(&m.startCalled, 1)
	<-m.stopCh // block until Stop
}

func (m *mockService) Stop() {
	atomic.StoreInt32(&m.stopCalled, 1)
	if m.stopDelay > 0 {
		time.Sleep(m.stopDelay)
	}
	m.stopOnce.Do(func() { close(m.stopCh) }) // unblock Start
}

// --- Basic tests ---

func TestServiceGroup_Start(t *testing.T) {
	svc1 := newMockService()
	svc2 := newMockService()

	sg := NewServiceGroup()
	sg.Add(svc1)
	sg.Add(svc2)

	go func() {
		time.Sleep(10 * time.Millisecond)
		sg.Stop()
	}()

	sg.Start()

	assert.Equal(t, int32(1), atomic.LoadInt32(&svc1.startCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc2.startCalled))
}

func TestServiceGroup_Stop(t *testing.T) {
	svc1 := newMockService()
	svc2 := newMockService()

	sg := NewServiceGroup()
	sg.Add(svc1)
	sg.Add(svc2)

	sg.Stop()

	assert.Equal(t, int32(1), atomic.LoadInt32(&svc1.stopCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc2.stopCalled))
}

func TestServiceGroup_StartThenStop(t *testing.T) {
	svc := newMockService()
	svc.stopDelay = 50 * time.Millisecond

	sg := NewServiceGroup()
	sg.Add(svc)

	go func() {
		time.Sleep(20 * time.Millisecond)
		sg.Stop()
	}()

	start := time.Now()
	sg.Start()
	elapsed := time.Since(start)

	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.startCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.stopCalled))
	assert.GreaterOrEqual(t, elapsed, 50*time.Millisecond)
}

func TestServiceGroup_StopOnce(t *testing.T) {
	var stopCount int32
	svc := &mockServiceWithCount{stopCount: &stopCount}

	sg := NewServiceGroup()
	sg.Add(svc)

	sg.Stop()
	sg.Stop()
	sg.Stop()

	assert.Equal(t, int32(1), atomic.LoadInt32(&stopCount))
}

type mockServiceWithCount struct {
	startCount int32
	stopCount  *int32
}

func (m *mockServiceWithCount) Start() {
	atomic.StoreInt32(&m.startCount, 1)
}

func (m *mockServiceWithCount) Stop() {
	atomic.AddInt32(m.stopCount, 1)
}

func TestServiceGroup_StopAllServices(t *testing.T) {
	svc1 := newMockService()
	svc2 := newMockService()
	svc3 := newMockService()

	sg := NewServiceGroup()
	sg.Add(svc1)
	sg.Add(svc2)
	sg.Add(svc3)

	sg.Stop()

	assert.Equal(t, int32(1), atomic.LoadInt32(&svc1.stopCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc2.stopCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc3.stopCalled))
}

func TestServiceGroup_Empty(t *testing.T) {
	sg := NewServiceGroup()
	sg.Start()
	sg.Stop()
}

// --- Panic tests ---

func TestServiceGroup_PanicInStart(t *testing.T) {
	// One healthy service plus one service that panics
	normal := newMockService()
	panicSvc := &panicService{panicMsg: "boom"}

	sg := NewServiceGroup()
	sg.Add(normal)
	sg.Add(panicSvc)

	// Start must not panic; it logs the error and returns normally
	assert.NotPanics(t, func() {
		sg.Start()
	})

	// The healthy service must have been stopped (the panic triggered stop)
	assert.Equal(t, int32(1), atomic.LoadInt32(&normal.stopCalled))
}

func TestServiceGroup_PanicInStart_OtherServicesUnblocked(t *testing.T) {
	// Verify that the other services are no longer blocked after the panic
	svc := newMockService()
	panicSvc := &panicService{panicMsg: "crash"}

	sg := NewServiceGroup()
	sg.Add(svc)
	sg.Add(panicSvc)

	done := make(chan struct{})
	go func() {
		defer close(done)
		sg.Start()
	}()

	select {
	case <-done:
		// Start has returned normally (the panic was recovered)
	case <-time.After(2 * time.Second):
		t.Fatal("Start blocked after panic, other services not unblocked")
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.stopCalled))
}

// --- Stop barrier ---

// TestServiceGroup_DoStopWaitsForStartBarrier is a white-box verification of the stop barrier:
// when Start has begun but a service has not entered Start yet, doStop must wait until the
// barrier is released before dispatching Stop.
//
// The barrier solves this: a Stop triggered by a panicking service would immediately act on
// the services whose Start has not been called, so their Stop runs first (a no-op for most
// real services), they enter Start afterwards and block forever, and since stopOnce is already
// exhausted nobody will ever call their Stop again.
func TestServiceGroup_DoStopWaitsForStartBarrier(t *testing.T) {
	svc := newMockService()
	sg := NewServiceGroup()
	sg.Add(svc)

	// Recreate the intermediate state "Start has begun but the service has not entered Start
	// yet": allEntered is not closed.
	sg.mu.Lock()
	sg.started = true
	sg.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sg.doStop()
	}()

	// Stop must not be called while the barrier is still closed
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&svc.stopCalled),
		"Stop must not be issued before every service has entered Start")

	// Release the barrier: doStop should continue and finish stopping
	close(sg.allEntered)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("doStop did not proceed after the barrier was released")
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.stopCalled))
}

// TestServiceGroup_DoStopWithoutStartSkipsBarrier verifies that doStop does not block forever
// on the barrier when Start was never called.
func TestServiceGroup_DoStopWithoutStartSkipsBarrier(t *testing.T) {
	svc := newMockService()
	sg := NewServiceGroup()
	sg.Add(svc)

	done := make(chan struct{})
	go func() {
		defer close(done)
		sg.doStop()
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("doStop blocked even though Start was never called")
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.stopCalled))
}

// TestServiceGroup_ManyServicesWithPanic verifies, with many services plus delayed starts plus
// a panic, that nothing deadlocks, nothing panics and every service ends up stopped.
func TestServiceGroup_ManyServicesWithPanic(t *testing.T) {
	sg := NewServiceGroup()
	const n = 12
	svcs := make([]*delayedStartService, 0, n)
	for i := 0; i < n; i++ {
		s := newDelayedStartService(time.Duration(i) * 5 * time.Millisecond)
		svcs = append(svcs, s)
		sg.Add(s)
	}
	sg.Add(&panicService{panicMsg: "boom"})

	done := make(chan struct{})
	go func() {
		defer close(done)
		sg.Start()
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Start blocked after panic with many services")
	}

	// Every service that entered Start must receive Stop (otherwise it blocks inside Start
	// forever)
	for i, s := range svcs {
		assert.Equal(t, int32(1), atomic.LoadInt32(&s.stopCalled), "service %d must be stopped", i)
	}
}

// delayedStartService waits a moment and then enters a blocking Start; Stop unblocks it by
// closing stopCh.
type delayedStartService struct {
	delay       time.Duration
	startCalled int32
	stopCalled  int32
	stopCh      chan struct{}
	stopOnce    sync.Once
}

func newDelayedStartService(delay time.Duration) *delayedStartService {
	return &delayedStartService{delay: delay, stopCh: make(chan struct{})}
}

func (s *delayedStartService) Start() {
	if s.delay > 0 {
		time.Sleep(s.delay)
	}
	atomic.StoreInt32(&s.startCalled, 1)
	<-s.stopCh
}

func (s *delayedStartService) Stop() {
	atomic.StoreInt32(&s.stopCalled, 1)
	s.stopOnce.Do(func() { close(s.stopCh) })
}

// TestServiceGroup_ConcurrentAddAndStart is a regression test: Add and Start running
// concurrently must not produce a data race.
// Historical defect: services was a bare slice, Add appended without synchronisation and
// Start/Stop iterated it in another goroutine, which -race could detect.
func TestServiceGroup_ConcurrentAddAndStart(t *testing.T) {
	sg := NewServiceGroup()
	sg.Add(newMockService())

	var wg sync.WaitGroup
	// Race with concurrent reads of the service list (the snapshot taken by doStart)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sg.Add(newMockService())
		}()
	}
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = sg.snapshot()
		}()
	}
	wg.Wait()

	// Stop everything to unblock all the mockServices
	sg.Stop()
}

// TestServiceGroup_AddAfterStartNotStarted verifies that a service added after Start is not
// part of the snapshot and therefore is neither started nor stopped (the documented lifecycle
// constraint).
func TestServiceGroup_AddAfterStartNotStarted(t *testing.T) {
	first := newMockService()
	sg := NewServiceGroup()
	sg.Add(first)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		sg.Stop()
	}()

	// Add concurrently while Start runs: the service is not part of the snapshot
	late := newMockService()
	time.AfterFunc(10*time.Millisecond, func() { sg.Add(late) })

	sg.Start()
	<-stopped

	assert.Equal(t, int32(1), atomic.LoadInt32(&first.startCalled), "first must have started")
	// Whether the late service starts depends on when the snapshot is taken; here we only
	// assert that nothing panics and nothing deadlocks
}

func TestServiceGroup_PanicInStart_MultiplePanics(t *testing.T) {
	// Several services panic at the same time: nothing panics and Start returns normally
	svc1 := &panicService{panicMsg: "first"}
	svc2 := &panicService{panicMsg: "second"}

	sg := NewServiceGroup()
	sg.Add(svc1)
	sg.Add(svc2)

	assert.NotPanics(t, func() {
		sg.Start()
	})
}

func TestServiceGroup_PanicInStop(t *testing.T) {
	// A panic in Stop must not crash the program
	normal := newMockService()
	panicSvc := &panicServiceStop{msg: "stop boom"}

	sg := NewServiceGroup()
	sg.Add(normal)
	sg.Add(panicSvc)

	// Stop must not panic
	assert.NotPanics(t, func() {
		sg.Stop()
	})

	// The healthy service must have been stopped
	assert.Equal(t, int32(1), atomic.LoadInt32(&normal.stopCalled))
}

func TestServiceGroup_PanicInStartAndStop(t *testing.T) {
	// A panic in Start plus a panic in Stop must not interfere with each other
	panicStart := &panicService{panicMsg: "start fail"}
	panicStop := &panicServiceStop{msg: "stop fail"}

	sg := NewServiceGroup()
	sg.Add(panicStart)
	sg.Add(panicStop)

	assert.NotPanics(t, func() {
		sg.Start()
	})
}

// --- panic Services used by the tests ---

// panicService panics inside Start.
type panicService struct {
	panicMsg any
}

func (p *panicService) Start() {
	panic(p.panicMsg)
}

func (p *panicService) Stop() {}

// panicServiceStop panics inside Stop.
type panicServiceStop struct {
	msg any
}

func (p *panicServiceStop) Start() {
	// Non-blocking, returns immediately
}

func (p *panicServiceStop) Stop() {
	panic(p.msg)
}

// --- WithStart tests ---

func TestWithStart(t *testing.T) {
	var started int32
	svc := WithStart(func() {
		atomic.StoreInt32(&started, 1)
	})

	svc.Start()
	assert.Equal(t, int32(1), atomic.LoadInt32(&started))

	assert.NotPanics(t, func() { svc.Stop() })
}

func TestWithStart_InServiceGroup(t *testing.T) {
	var started int32

	sg := NewServiceGroup()
	sg.Add(WithStart(func() {
		atomic.StoreInt32(&started, 1)
	}))

	sg.Start()
	assert.Equal(t, int32(1), atomic.LoadInt32(&started))

	sg.Stop()
}

// --- WithStarter tests ---

func TestWithStarter(t *testing.T) {
	var started int32
	starter := &mockStarter{started: &started}

	svc := WithStarter(starter)
	svc.Start()
	assert.Equal(t, int32(1), atomic.LoadInt32(&started))

	assert.NotPanics(t, func() { svc.Stop() })
}

type mockStarter struct {
	started *int32
}

func (m *mockStarter) Start() {
	atomic.StoreInt32(m.started, 1)
}

// --- Concurrency safety tests ---

func TestServiceGroup_ConcurrentStop(t *testing.T) {
	svc := newMockService()
	sg := NewServiceGroup()
	sg.Add(svc)

	go func() {
		time.Sleep(10 * time.Millisecond)
		sg.Stop()
	}()

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sg.Stop()
		}()
	}

	sg.Start()
	wg.Wait()

	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.stopCalled))
}

// --- AsService tests (adapting Start() error + Stop() error objects) ---

// errStartService simulates an object with Start() error + Stop() error (shaped like
// *httpx.Server).
// With block=true, Start blocks until Stop is called (close to real server semantics).
type errStartService struct {
	startCalled int32
	stopCalled  int32
	startErr    error
	stopErr     error
	block       bool
	stopCh      chan struct{}
	stopOnce    sync.Once
}

func newBlockingErrStartService() *errStartService {
	return &errStartService{block: true, stopCh: make(chan struct{})}
}

func (m *errStartService) Start() error {
	atomic.StoreInt32(&m.startCalled, 1)
	if m.block {
		<-m.stopCh
	}
	return m.startErr
}

func (m *errStartService) Stop() error {
	atomic.StoreInt32(&m.stopCalled, 1)
	if m.stopCh != nil {
		m.stopOnce.Do(func() { close(m.stopCh) })
	}
	return m.stopErr
}

func TestAsService_StartStop(t *testing.T) {
	s := &errStartService{}
	svc := AsService(s)

	svc.Start()
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.startCalled))

	svc.Stop()
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.stopCalled))
}

func TestAsService_StartErrorDoesNotPanic(t *testing.T) {
	s := &errStartService{startErr: errors.New("boom")}
	svc := AsService(s)

	assert.NotPanics(t, func() { svc.Start() }) // the error is only logged, no panic
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.startCalled))
}

func TestAsService_StopErrorDoesNotPanic(t *testing.T) {
	s := &errStartService{stopErr: errors.New("stop boom")}
	svc := AsService(s)

	assert.NotPanics(t, func() { svc.Stop() }) // the error is only logged, no panic
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.stopCalled))
}

func TestAsService_InServiceGroup(t *testing.T) {
	s := newBlockingErrStartService()

	sg := NewServiceGroup()
	sg.Add(AsService(s))

	go func() {
		time.Sleep(10 * time.Millisecond)
		sg.Stop()
	}()

	sg.Start() // Start blocks until Stop releases it 10ms later
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.startCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.stopCalled))
}
