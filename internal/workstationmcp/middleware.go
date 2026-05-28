package workstationmcp

import (
	"context"
	"fmt"
)

func DryRunPolicy() Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req Request) (Response, error) {
			resp, err := next(ctx, req)
			if err != nil {
				return Response{}, err
			}
			resp.DryRun = true
			if req.Params["execute"] == "true" {
				resp.Warnings = append(resp.Warnings, "live execution is intentionally unsupported in this public sample")
				resp.NextSteps = append(resp.NextSteps, "keep workstation actions in review-only dry-run mode")
			}
			return resp, nil
		}
	}
}

func ResponseBudget(maxPayloadValues int) Middleware {
	return func(next Handler) Handler {
		return func(ctx context.Context, req Request) (Response, error) {
			resp, err := next(ctx, req)
			if err != nil {
				return Response{}, err
			}
			if maxPayloadValues <= 0 || len(resp.Payload) <= maxPayloadValues {
				return resp, nil
			}
			trimmed := make(map[string]any, maxPayloadValues)
			kept := 0
			for key, value := range resp.Payload {
				if kept == maxPayloadValues {
					break
				}
				trimmed[key] = value
				kept++
			}
			resp.Payload = trimmed
			resp.Warnings = append(resp.Warnings, fmt.Sprintf("payload trimmed to %d fields", maxPayloadValues))
			return resp, nil
		}
	}
}
