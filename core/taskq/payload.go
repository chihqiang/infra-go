package taskq

import (
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"
)

// MarshalPayload marshals any value into JSON for use as a task payload.
func MarshalPayload(v any) ([]byte, error) {
	data, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("taskq: marshal payload: %w", err)
	}
	return data, nil
}

// UnmarshalPayload unmarshals the task payload into the target value.
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
