package service

import (
	"fmt"
	"sync"

	"github.com/chihqiang/infra-go/logger"
)

// --- 接口定义 ---

// Starter 包装 Start 方法，用于启动服务。
type Starter interface {
	Start()
}

// Stopper 包装 Stop 方法，用于停止服务。
type Stopper interface {
	Stop()
}

// Service 是同时具备 Start 和 Stop 能力的服务接口。
//
// 实现契约（重要）：
//   - Start 通常阻塞直到服务退出（例如等待停止信号）。
//   - Stop 可能在任何时刻被调用，包括与 Start 并发、或紧接 Start 开始之后。
//     实现必须幂等，并能容忍"停止请求早于服务自身完成初始化"的情况
//     （例如先记录停止意图，待初始化完成后再执行实际关闭）。
//
// ServiceGroup 保证 Stop 至多下发一次，且不会早于该服务的 Start 被调用
// （见 ServiceGroup.doStop 的启动屏障）；但无法保证 Stop 一定晚于
// Start 内部的初始化完成，这段窗口需由各服务自行处理。
type Service interface {
	Starter
	Stopper
}

// --- ServiceGroup ---

// ServiceGroup 管理一组 Service，支持并发启动和并发停止。
//
// 启动：所有 Service 并发调用 Start，ServiceGroup.Start 阻塞直到全部返回。
// 停止：所有 Service 并发调用 Stop，Stop 保证只执行一次（sync.Once）。
// 顺序：Add 追加到尾部（O(1)），启动按添加顺序并发，停止时按添加的逆序并发。
// Panic：Start 和 Stop 中的 panic 都会被恢复，通过 logger 记录错误日志，
// 不中断其他服务。Start 中 panic 会自动触发 Stop 解除其他服务阻塞。
//
// 生命周期约束：Add 必须在 Start 之前调用。Start 之后再 Add 的服务不会被启动，
// 也不会被停止（Start/Stop 在下发时对服务列表取快照）。
//
// 典型用法：
//
//	sg := service.NewServiceGroup()
//	sg.Add(httpService)
//	sg.Add(redisService)
//	sg.Start() // 阻塞，所有服务退出后返回
type ServiceGroup struct {
	mu       sync.Mutex
	services []Service
	// started 标记 doStart 已开始，供 doStop 判断是否需要等待启动屏障。
	started bool
	// allEntered 在所有服务 goroutine 进入 Start 之前关闭，作为停止屏障。
	allEntered chan struct{}
	stopOnce   func()
}

// NewServiceGroup 创建一个 ServiceGroup。
func NewServiceGroup() *ServiceGroup {
	sg := &ServiceGroup{
		allEntered: make(chan struct{}),
	}
	sg.stopOnce = sync.OnceFunc(sg.doStop)
	return sg
}

// Add 将 service 添加到组中。
// 追加到尾部（O(1)），启动按添加顺序，停止时按逆序（后添加的先停止）。
//
// 必须在 Start 之前调用：Start 会对服务列表取快照，
// 之后添加的服务不会被启动，也不会被停止。
// 并发调用安全，但并发 Add 与 Start 的先后顺序不确定。
func (sg *ServiceGroup) Add(service Service) {
	sg.mu.Lock()
	sg.services = append(sg.services, service)
	sg.mu.Unlock()
}

// snapshot 返回服务列表的副本，避免持有锁执行用户代码（Start/Stop）。
func (sg *ServiceGroup) snapshot() []Service {
	sg.mu.Lock()
	defer sg.mu.Unlock()
	// 拷贝一份：即便调用方在 Start 期间并发 Add，也不会改写到正在遍历的切片。
	out := make([]Service, len(sg.services))
	copy(out, sg.services)
	return out
}

// Start 并发启动所有 Service，阻塞直到全部退出。
// 如果某个 Service 在 Start 中 panic，会自动触发 Stop 停止其他服务，
// 并通过 logger 记录错误日志（含服务索引与类型名），不会重新 panic。
//
// 停止屏障：doStop 会等待所有服务 goroutine 进入 Start 之后才下发 Stop，
// 避免"服务尚未启动就被 Stop，随后启动却再也收不到停止信号"。
// 调用此方法后不应再有任何后续逻辑代码。
func (sg *ServiceGroup) Start() {
	sg.doStart()
}

// Stop 并发停止所有 Service，保证只执行一次。
func (sg *ServiceGroup) Stop() {
	sg.stopOnce()
}

// doStart 并发启动所有 Service 并等待全部退出。
// Start 中的 panic 会被恢复，触发 Stop 解除其他服务阻塞，通过 logger 记录错误。
func (sg *ServiceGroup) doStart() {
	services := sg.snapshot()

	// 标记启动已开始：doStop 据此决定是否需要等待启动屏障。
	// 必须在启动 goroutine 之前设置，否则 Stop 可能在标记前到达并跳过等待。
	sg.mu.Lock()
	sg.started = true
	sg.mu.Unlock()

	// 启动屏障计数：每个服务在调用 Start 之前 Done 一次。
	// 当所有服务都已进入 Start，关闭 allEntered 允许 doStop 下发停止。
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
						// 同步触发停止，确保其他服务的 Start 阻塞被解除。
						// doStop 会先等待所有服务进入 Start，因此不会出现
						// "Stop 先于 Start" 导致服务永远收不到停止信号。
						sg.stopOnce()
					})
				}
			}()
			// 在进入 Start 之前上报，使停止屏障能在 Start 阻塞时正常放行。
			entered.Done()
			s.Start()
		}(i, svc)
	}
	wg.Wait()
}

// doStop 并发停止所有 Service 并等待全部完成。
// Stop 中的 panic 只记录，不中断其他服务的停止。
func (sg *ServiceGroup) doStop() {
	// 等待所有服务进入 Start 后再下发停止。
	//
	// 不等待的话，某个服务 panic 触发的 Stop 会作用于"尚未启动"的服务：
	// 它们的 Stop 先被调用（多半是空操作），随后才执行 Start，
	// 而 stopOnce 已耗尽，这些服务启动后将永远收不到停止信号
	// （阻塞在 Start 中，进程无法退出）。
	//
	// 仅当 Start 已开始才等待：否则 allEntered 永远不会关闭，Stop 会永久阻塞。
	sg.mu.Lock()
	started := sg.started
	sg.mu.Unlock()
	if started {
		<-sg.allEntered
	}

	var wg sync.WaitGroup
	// 逆序遍历：后添加的服务先停止
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

// --- 适配函数 ---

// WithStart 将一个 start func 包装为 Service（Stop 为空操作）。
func WithStart(start func()) Service {
	return startOnlyService{start: start}
}

// WithStarter 将一个 Starter 包装为 Service（Stop 为空操作）。
func WithStarter(start Starter) Service {
	return starterOnlyService{Starter: start}
}

// --- 内部适配类型 ---

// noopStopper 是一个 Stop 为空操作的 Stopper 实现。
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

// --- error 型服务适配 ---

// AsService 将实现了 Start() error 与 Stop() error 的对象适配为 Service，便于直接纳入 ServiceGroup。
// 典型对象如 *httpx.Server（Start/Stop 均返回 error，签名与 Service.Starter/Stopper 不同）。
//
//	sg := service.NewServiceGroup()
//	sg.Add(service.AsService(srv)) // srv *httpx.Server
//	sg.Start()
//
// Start / Stop 返回的 error 都会记录为错误日志（Service 接口无返回值，无法向上传递）；
// Start 中的 panic 仍由 ServiceGroup 统一恢复并触发 Stop。
func AsService[T interface {
	Start() error
	Stop() error
}](s T) Service {
	return errorServiceAdapter[T]{s: s}
}

// errorServiceAdapter 把 Start() error + Stop() error 的对象包装为无返回值的 Service。
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
