package service

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// --- 测试用 Service ---

// mockService 模拟一个服务：Start 阻塞直到 Stop 被调用。
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
	<-m.stopCh // 阻塞直到 Stop
}

func (m *mockService) Stop() {
	atomic.StoreInt32(&m.stopCalled, 1)
	if m.stopDelay > 0 {
		time.Sleep(m.stopDelay)
	}
	m.stopOnce.Do(func() { close(m.stopCh) }) // 解除 Start 阻塞
}

// --- 基础测试 ---

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

// --- Panic 测试 ---

func TestServiceGroup_PanicInStart(t *testing.T) {
	// 一个正常服务 + 一个会 panic 的服务
	normal := newMockService()
	panicSvc := &panicService{panicMsg: "boom"}

	sg := NewServiceGroup()
	sg.Add(normal)
	sg.Add(panicSvc)

	// Start 不应 panic，而是记录日志后正常返回
	assert.NotPanics(t, func() {
		sg.Start()
	})

	// 正常服务应该被 Stop 了（panic 触发了 stop）
	assert.Equal(t, int32(1), atomic.LoadInt32(&normal.stopCalled))
}

func TestServiceGroup_PanicInStart_OtherServicesUnblocked(t *testing.T) {
	// 验证 panic 后其他服务不再阻塞
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
		// Start 已正常返回（panic 被恢复）
	case <-time.After(2 * time.Second):
		t.Fatal("Start blocked after panic, other services not unblocked")
	}

	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.stopCalled))
}

// --- 停止屏障 ---

// TestServiceGroup_DoStopWaitsForStartBarrier 白盒验证停止屏障：
// Start 已开始但服务尚未进入 Start 时，doStop 必须等待屏障放行后才下发 Stop。
//
// 该屏障解决的是：某服务 panic 触发的 Stop 会立即作用于"Start 尚未被调用"的服务，
// 它们的 Stop 先执行（对多数真实服务是空操作），随后才进入 Start 并永久阻塞，
// 而 stopOnce 已耗尽，再也不会有人调用它们的 Stop。
func TestServiceGroup_DoStopWaitsForStartBarrier(t *testing.T) {
	svc := newMockService()
	sg := NewServiceGroup()
	sg.Add(svc)

	// 构造"Start 已开始、但服务还没进入 Start"的中间态：allEntered 未关闭。
	sg.mu.Lock()
	sg.started = true
	sg.mu.Unlock()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sg.doStop()
	}()

	// 屏障未放行前不得调用 Stop
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, int32(0), atomic.LoadInt32(&svc.stopCalled),
		"Stop must not be issued before every service has entered Start")

	// 放行屏障：doStop 应继续并完成停止
	close(sg.allEntered)

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("doStop did not proceed after the barrier was released")
	}
	assert.Equal(t, int32(1), atomic.LoadInt32(&svc.stopCalled))
}

// TestServiceGroup_DoStopWithoutStartSkipsBarrier 验证未调用 Start 时
// doStop 不会因等待屏障而永久阻塞。
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

// TestServiceGroup_ManyServicesWithPanic 在较多服务 + 延迟启动 + panic 的组合下
// 验证不会死锁、不会 panic，且所有服务最终都被停止。
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

	// 所有已进入 Start 的服务都必须收到 Stop（否则会永久阻塞在 Start 内）
	for i, s := range svcs {
		assert.Equal(t, int32(1), atomic.LoadInt32(&s.stopCalled), "service %d must be stopped", i)
	}
}

// delayedStartService 延迟片刻后进入阻塞的 Start，Stop 通过关闭 stopCh 解除阻塞。
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

// TestServiceGroup_ConcurrentAddAndStart 回归测试：Add 与 Start 并发不产生数据竞争。
// 历史缺陷：services 是裸切片，Add 无同步追加，Start/Stop 在其它 goroutine 中遍历，
// -race 可检出。
func TestServiceGroup_ConcurrentAddAndStart(t *testing.T) {
	sg := NewServiceGroup()
	sg.Add(newMockService())

	var wg sync.WaitGroup
	// 与服务列表并发读取（doStart 的 snapshot）竞争
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

	// 停止以解除所有 mockService 的阻塞
	sg.Stop()
}

// TestServiceGroup_AddAfterStartNotStarted 验证 Start 后新增的服务不在快照内，
// 因而不会被启动或停止（文档化的生命周期约束）。
func TestServiceGroup_AddAfterStartNotStarted(t *testing.T) {
	first := newMockService()
	sg := NewServiceGroup()
	sg.Add(first)

	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		sg.Stop()
	}()

	// 在 Start 期间并发 Add：该服务不在快照内
	late := newMockService()
	time.AfterFunc(10*time.Millisecond, func() { sg.Add(late) })

	sg.Start()
	<-stopped

	assert.Equal(t, int32(1), atomic.LoadInt32(&first.startCalled), "first must have started")
	// 后加服务的启动与否取决于快照时机，此处只断言不 panic、无死锁
}

func TestServiceGroup_PanicInStart_MultiplePanics(t *testing.T) {
	// 多个服务同时 panic，不 panic，正常返回
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
	// Stop 中的 panic 不应导致程序崩溃
	normal := newMockService()
	panicSvc := &panicServiceStop{msg: "stop boom"}

	sg := NewServiceGroup()
	sg.Add(normal)
	sg.Add(panicSvc)

	// Stop 不应 panic
	assert.NotPanics(t, func() {
		sg.Stop()
	})

	// 正常服务应该被停止了
	assert.Equal(t, int32(1), atomic.LoadInt32(&normal.stopCalled))
}

func TestServiceGroup_PanicInStartAndStop(t *testing.T) {
	// Start panic + Stop panic，不应互相干扰
	panicStart := &panicService{panicMsg: "start fail"}
	panicStop := &panicServiceStop{msg: "stop fail"}

	sg := NewServiceGroup()
	sg.Add(panicStart)
	sg.Add(panicStop)

	assert.NotPanics(t, func() {
		sg.Start()
	})
}

// --- 测试用 panic Service ---

// panicService 在 Start 中 panic。
type panicService struct {
	panicMsg any
}

func (p *panicService) Start() {
	panic(p.panicMsg)
}

func (p *panicService) Stop() {}

// panicServiceStop 在 Stop 中 panic。
type panicServiceStop struct {
	msg any
}

func (p *panicServiceStop) Start() {
	// 非阻塞，立即返回
}

func (p *panicServiceStop) Stop() {
	panic(p.msg)
}

// --- WithStart 测试 ---

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

// --- WithStarter 测试 ---

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

// --- 并发安全测试 ---

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

// --- AsService 测试（Start() error + Stop() error 对象适配）---

// errStartService 模拟 Start() error + Stop() error 的对象（形如 *httpx.Server）。
// block=true 时 Start 阻塞直到 Stop（贴近真实服务器语义）。
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

	assert.NotPanics(t, func() { svc.Start() }) // 错误仅记录日志，不 panic
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.startCalled))
}

func TestAsService_StopErrorDoesNotPanic(t *testing.T) {
	s := &errStartService{stopErr: errors.New("stop boom")}
	svc := AsService(s)

	assert.NotPanics(t, func() { svc.Stop() }) // 错误仅记录日志，不 panic
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

	sg.Start() // Start 阻塞，直到 10ms 后 Stop 解除
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.startCalled))
	assert.Equal(t, int32(1), atomic.LoadInt32(&s.stopCalled))
}
