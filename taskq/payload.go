package taskq

import (
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

// MarshalPayload 将任意值序列化为 JSON，用作任务 payload。
func MarshalPayload(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("taskq: marshal payload: %w", err)
	}
	return data, nil
}

// UnmarshalPayload 将任务 payload 反序列化到目标值。
func UnmarshalPayload(task *asynq.Task, v any) error {
	if task == nil {
		return fmt.Errorf("taskq: task is nil")
	}
	if len(task.Payload()) == 0 {
		return nil
	}
	if err := json.Unmarshal(task.Payload(), v); err != nil {
		return fmt.Errorf("taskq: unmarshal payload: %w", err)
	}
	return nil
}
