package health

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/7irelo/helmforge/internal/core/model"
	"github.com/7irelo/helmforge/internal/util/log"
)

// Checker performs health checks against deployed services.
type Checker interface {
	Check(ctx context.Context, hc model.HealthCheck) error
}

type httpChecker struct {
	client *http.Client
}

// NewChecker returns a health checker.
func NewChecker() Checker {
	return &httpChecker{
		client: &http.Client{Timeout: 5 * time.Second},
	}
}

func (c *httpChecker) Check(ctx context.Context, hc model.HealthCheck) error {
	switch hc.Type {
	case "http":
		return c.checkHTTP(ctx, hc)
	case "none", "":
		log.L().Debug().Msg("no health check configured, skipping")
		return nil
	default:
		return fmt.Errorf("unsupported health check type: %s", hc.Type)
	}
}

func (c *httpChecker) checkHTTP(ctx context.Context, hc model.HealthCheck) error {
	timeout := time.Duration(hc.TimeoutSeconds) * time.Second
	deadline := time.Now().Add(timeout)
	interval := 2 * time.Second

	log.L().Info().Str("url", hc.URL).Int("timeout_seconds", hc.TimeoutSeconds).Msg("starting health check")

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("health check timed out after %ds for %s", hc.TimeoutSeconds, hc.URL)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, hc.URL, nil)
		if err != nil {
			return fmt.Errorf("create health check request: %w", err)
		}

		resp, err := c.client.Do(req)
		if err != nil {
			log.L().Debug().Err(err).Str("url", hc.URL).Msg("health check attempt failed")
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(interval):
				continue
			}
		}
		resp.Body.Close()

		if resp.StatusCode == http.StatusOK {
			log.L().Info().Str("url", hc.URL).Msg("health check passed")
			return nil
		}

		log.L().Debug().Int("status", resp.StatusCode).Str("url", hc.URL).Msg("health check not yet healthy")
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(interval):
		}
	}
}
