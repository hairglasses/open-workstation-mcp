package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/hairglasses/open-workstation-mcp/internal/mcpcompat/registry"
	"github.com/invopop/jsonschema"
	"github.com/mark3labs/mcp-go/mcp"
)

const (
	ErrClientInit    = "CLIENT_INIT_FAILED"
	ErrInvalidParam  = "INVALID_PARAM"
	ErrTimeout       = "TIMEOUT"
	ErrNotFound      = "NOT_FOUND"
	ErrAPIError      = "API_ERROR"
	ErrPermission    = "PERMISSION_DENIED"
	ErrValidation    = "OUTPUT_VALIDATION_FAILED"
	ErrInternal      = "INTERNAL"
	ErrRateLimited   = "RATE_LIMITED"
	ErrUpstreamError = "UPSTREAM_ERROR"
	ErrConflict      = "CONFLICT"
)

type TypedHandlerFunc[In any, Out any] func(ctx context.Context, input In) (Out, error)

func TypedHandler[In any, Out any](name, description string, fn TypedHandlerFunc[In, Out]) registry.ToolDefinition {
	inputSchema := generateInputSchema[In]()
	outputSchema := generateOutputSchema[Out]()

	wrapped := func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
		var input In
		if req.Params.Arguments != nil {
			argBytes, err := json.Marshal(req.Params.Arguments)
			if err != nil {
				return ErrorResult(fmt.Errorf("failed to marshal arguments: %w", err)), nil
			}
			if err := json.Unmarshal(argBytes, &input); err != nil {
				hint := ""
				if hb, e := json.Marshal(inputSchema); e == nil {
					hint = "\nExpected schema: " + string(hb)
				}
				return CodedErrorResult(ErrInvalidParam, fmt.Errorf("failed to parse arguments: %w%s", err, hint)), nil
			}
		}

		output, err := fn(ctx, input)
		if err != nil {
			return ErrorResult(err), nil
		}

		return StructuredResult(output), nil
	}

	return registry.ToolDefinition{
		Tool: mcp.Tool{
			Name:        name,
			Description: description,
			InputSchema: mcp.ToolInputSchema(inputSchema),
		},
		Handler:      wrapped,
		OutputSchema: outputSchema,
	}
}

func generateInputSchema[T any]() mcp.ToolArgumentsSchema {
	props, required := reflectSchemaProperties[T]()
	return mcp.ToolArgumentsSchema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

func generateOutputSchema[T any]() *mcp.ToolOutputSchema {
	props, required := reflectSchemaProperties[T]()
	return &mcp.ToolOutputSchema{
		Type:       "object",
		Properties: props,
		Required:   required,
	}
}

func reflectSchemaProperties[T any]() (map[string]any, []string) {
	r := jsonschema.Reflector{DoNotReference: true}
	schema := r.Reflect(new(T))

	properties := make(map[string]any)
	if schema.Properties != nil {
		for pair := schema.Properties.Oldest(); pair != nil; pair = pair.Next() {
			properties[pair.Key] = schemaToMap(pair.Value)
		}
	}
	var required []string
	if len(schema.Required) > 0 {
		required = schema.Required
	}
	return properties, required
}

func schemaToMap(s *jsonschema.Schema) map[string]any {
	m := make(map[string]any)

	if s.Type != "" {
		m["type"] = s.Type
	}
	if s.Description != "" {
		m["description"] = s.Description
	}
	if s.Format != "" {
		m["format"] = s.Format
	}
	if len(s.Enum) > 0 {
		m["enum"] = s.Enum
	}
	if s.Default != nil {
		m["default"] = s.Default
	}

	if s.Type == "array" {
		if s.Items != nil {
			itemMap := schemaToMap(s.Items)
			if _, hasType := itemMap["type"]; !hasType || itemMap["type"] == "" {
				itemMap["type"] = "string"
			}
			m["items"] = itemMap
		} else {
			m["items"] = map[string]any{"type": "string"}
		}
	} else if s.Items != nil && s.Items.Type != "" {
		m["items"] = schemaToMap(s.Items)
	}

	if s.Properties != nil && s.Properties.Len() > 0 {
		props := make(map[string]any)
		for pair := s.Properties.Oldest(); pair != nil; pair = pair.Next() {
			props[pair.Key] = schemaToMap(pair.Value)
		}
		m["properties"] = props
		if len(s.Required) > 0 {
			m["required"] = s.Required
		}
	}

	return m
}

func TextResult(text string) *registry.CallToolResult {
	return registry.MakeTextResult(text)
}

func ErrorResult(err error) *registry.CallToolResult {
	return registry.MakeErrorResult(err.Error())
}

func JSONResult(data any) *registry.CallToolResult {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return ErrorResult(err)
	}
	return registry.MakeTextResult(string(bytes))
}

func CodedErrorResult(code string, err error) *registry.CallToolResult {
	return registry.MakeErrorResult(fmt.Sprintf("[%s] %s", code, err.Error()))
}

func ActionableErrorResult(code string, err error, suggestions ...string) *registry.CallToolResult {
	var msg strings.Builder
	fmt.Fprintf(&msg, "[%s] %s", code, err.Error())
	if len(suggestions) > 0 {
		msg.WriteString("\n\nSuggested actions:")
		for _, s := range suggestions {
			msg.WriteString("\n  - " + s)
		}
	}
	return registry.MakeErrorResult(msg.String())
}

func StructuredResult(data any) *registry.CallToolResult {
	bytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return ErrorResult(fmt.Errorf("failed to marshal structured result: %w", err))
	}
	return registry.MakeStructuredResult(registry.MakeTextContent(string(bytes)), data)
}
