package service

import (
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
