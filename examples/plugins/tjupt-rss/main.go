package main

import (
	"encoding/json"
	"io"
	"os"
)

type request struct {
	Action    string          `json:"action"`
	Settings  json.RawMessage `json:"settings"`
	Resources json.RawMessage `json:"resources"`
}

type response struct {
	HostAction string `json:"host_action"`
	Blocks     []any  `json:"blocks"`
}

var allowedActions = map[string]struct{}{
	"preview": {},
	"status":  {},
	"trial":   {},
	"start":   {},
	"stop":    {},
}

func main() {
	decoder := json.NewDecoder(io.LimitReader(os.Stdin, 1<<20))
	decoder.DisallowUnknownFields()
	var input request
	if err := decoder.Decode(&input); err != nil {
		os.Exit(1)
	}
	if _, ok := allowedActions[input.Action]; !ok {
		os.Exit(1)
	}
	if err := decoder.Decode(new(json.RawMessage)); err != io.EOF {
		os.Exit(1)
	}
	if err := json.NewEncoder(os.Stdout).Encode(response{
		HostAction: input.Action,
		Blocks:     []any{},
	}); err != nil {
		os.Exit(1)
	}
}
