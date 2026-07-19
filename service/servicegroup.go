package service

import (
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
type Service interface {
	Starter
	Stopper
}

// --- ServiceGroup ---

// ServiceGroup 管理一组 Service，支持并发启动和并发停止。
//
// 启动：所有 Service 并发调用 Start，ServiceGroup.Start 阻塞直到全部返回。
// 停止：所有 Service 并发调用 Stop，Stop 保证只执行一次（sync.Once）。
// 顺序：Add 时插入到头部，停止时按添加的逆序停止。
// Panic：Start 和 Stop 中的 panic 都会被恢复，通过 logger 记录错误日志，
// 不中断其他服务。Start 中 panic 会自动触发 Stop 解除其他服务阻塞。
//
// 典型用法：
//
//	sg := service.NewServiceGroup()
//	sg.Add(httpService)
//	sg.Add(redisService)
//	sg.Start() // 阻塞，所有服务退出后返回
type ServiceGroup struct {
	services []Service
	stopOnce func()
}

// NewServiceGroup 创建一个 ServiceGroup。
func NewServiceGroup() *ServiceGroup {
	sg := new(ServiceGroup)
	sg.stopOnce = sync.OnceFunc(sg.doStop)
	return sg
}

// Add 将 service 添加到组中。
// 添加到头部，停止时按添加的逆序停止（后添加的先停止）。
func (sg *ServiceGroup) Add(service Service) {
	sg.services = append([]Service{service}, sg.services...)
}

// Start 并发启动所有 Service，阻塞直到全部退出。
// 如果某个 Service 在 Start 中 panic，会自动触发 Stop 停止其他服务，
// 并通过 logger 记录错误日志，不会重新 panic。
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
	var wg sync.WaitGroup
	var panicOnce sync.Once

	for _, svc := range sg.services {
		wg.Add(1)
		go func(s Service) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					panicOnce.Do(func() {
						logger.Errorf("service: panic during start: %v", r)
						// 同步触发停止，确保其他服务的 Start 阻塞被解除
						sg.stopOnce()
					})
				}
			}()
			s.Start()
		}(svc)
	}
	wg.Wait()
}

// doStop 并发停止所有 Service 并等待全部完成。
// Stop 中的 panic 只记录，不中断其他服务的停止。
func (sg *ServiceGroup) doStop() {
	var wg sync.WaitGroup
	for _, svc := range sg.services {
		wg.Add(1)
		go func(s Service) {
			defer wg.Done()
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("service: panic during stop: %v", r)
				}
			}()
			s.Stop()
		}(svc)
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
