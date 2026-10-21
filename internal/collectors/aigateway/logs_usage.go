package aigateway

import (
	"encoding/json"
	"math"
)

type responseUsage struct {
	Input      *int64 `json:"input_tokens"`
	Output     *int64 `json:"output_tokens"`
	Prompt     *int64 `json:"prompt_tokens"`
	Completion *int64 `json:"completion_tokens"`
}

// responseTokenUsage accepts Workers AI's result.usage and the top-level
// usage shapes. Decode integers directly so fractions, strings and overflow
// cannot silently become metric counts. Never include response text in errors.
func responseTokenUsage(body json.RawMessage) (input, output *int64) {
	var envelope struct {
		Usage  json.RawMessage `json:"usage"`
		Result *struct {
			Usage json.RawMessage `json:"usage"`
		} `json:"result"`
	}
	if json.Unmarshal(body, &envelope) != nil {
		return nil, nil
	}
	raw := envelope.Usage
	if (len(raw) == 0 || string(raw) == "null") && envelope.Result != nil {
		raw = envelope.Result.Usage
	}
	var usage responseUsage
	if json.Unmarshal(raw, &usage) != nil {
		return nil, nil
	}
	for _, n := range []*int64{usage.Input, usage.Output, usage.Prompt, usage.Completion} {
		if n != nil && *n < 0 {
			return nil, nil
		}
	}
	input, output = usage.Input, usage.Output
	if input == nil {
		input = usage.Prompt
	}
	if output == nil {
		output = usage.Completion
	}
	return input, output
}

// resolveResponseUsage preserves positive gateway counts and fills each
// missing or zero side independently. Total is recomputed only when absent or
// zero, both effective sides are known and the sum fits in an int64.
func resolveResponseUsage(input, output, total **int64, body json.RawMessage) bool {
	bodyInput, bodyOutput := responseTokenUsage(body)
	derived := false
	for _, side := range []struct {
		current **int64
		body    *int64
	}{{input, bodyInput}, {output, bodyOutput}} {
		if (*side.current == nil || **side.current == 0) && side.body != nil {
			*side.current = side.body
			derived = true
		}
	}
	if derived && (*total == nil || **total == 0) && *input != nil && *output != nil && **input >= 0 && **output >= 0 && **input <= math.MaxInt64-**output {
		sum := **input + **output
		*total = &sum
	}
	return derived
}
