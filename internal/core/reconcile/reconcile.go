package reconcile

import (
	"context"
	"encoding/json"
	"fmt"
	"path"
	"sync"
	"time"

	"github.com/7irelo/helmforge/internal/adapters/health"
	"github.com/7irelo/helmforge/internal/adapters/remote"
	"github.com/7irelo/helmforge/internal/adapters/store"
	"github.com/7irelo/helmforge/internal/core/model"
	"github.com/7irelo/helmforge/internal/util/log"
)

// Engine executes deployment plans against remote hosts.
type Engine struct {
	Remote  remote.Runner
	Health  health.Checker
	Store   store.ReleaseStore
}

// ApplyInput holds parameters for Apply.
type ApplyInput struct {
	Plan        *model.DeployPlan
	MaxParallel int
	LocalAppDir string // local directory containing compose files
}

// Apply executes the deployment plan with a rolling strategy.
func (e *Engine) Apply(ctx context.Context, input ApplyInput) (*model.Release, error) {
	cfg := input.Plan.Config
	if input.MaxParallel <= 0 {
		input.MaxParallel = 1
	}

	release := &model.Release{
		ID:        generateReleaseID(),
		Env:       input.Plan.Env,
		App:       input.Plan.App,
		Repo:      input.Plan.Repo,
		Ref:       input.Plan.Ref,
		CommitSHA: input.Plan.CommitSHA,
		StartedAt: time.Now().UTC(),
		Status:    model.ReleaseStatusRunning,
	}

	if err := e.Store.SaveRelease(release); err != nil {
		return nil, fmt.Errorf("save release: %w", err)
	}

	sem := make(chan struct{}, input.MaxParallel)
	var mu sync.Mutex
	failed := false

	for _, target := range cfg.Targets {
		// Check for cancellation or prior failure.
		select {
		case <-ctx.Done():
			mu.Lock()
			release.Status = model.ReleaseStatusCancelled
			release.FinishedAt = time.Now().UTC()
			e.Store.UpdateRelease(release)
			mu.Unlock()
			return release, ctx.Err()
		default:
		}

		mu.Lock()
		if failed {
			mu.Unlock()
			break
		}
		mu.Unlock()

		sem <- struct{}{} // acquire slot

		// For rolling strategy, run sequentially (wait for slot to be freed).
		target := target
		hr := e.deployToHost(ctx, target, input, release)

		mu.Lock()
		release.HostResults = append(release.HostResults, hr)
		if hr.Status == model.ReleaseStatusFailed {
			failed = true
		}
		mu.Unlock()

		<-sem // release slot
	}

	release.FinishedAt = time.Now().UTC()
	if failed {
		release.Status = model.ReleaseStatusFailed
	} else {
		release.Status = model.ReleaseStatusSuccess
	}

	if err := e.Store.UpdateRelease(release); err != nil {
		log.L().Error().Err(err).Msg("failed to update release")
	}

	return release, nil
}

func (e *Engine) deployToHost(ctx context.Context, target model.Target, input ApplyInput, release *model.Release) model.HostResult {
	hr := model.HostResult{
		Host:      target.Host,
		StartedAt: time.Now().UTC(),
		Status:    model.ReleaseStatusRunning,
	}

	cfg := input.Plan.Config
	logger := log.L().With().Str("host", target.Host).Logger()

	// Step 1: Ensure remote directory.
	logger.Info().Str("path", target.Path).Msg("ensuring remote directory")
	if _, err := e.Remote.Run(ctx, target, fmt.Sprintf("mkdir -p %s", shellQuote(target.Path))); err != nil {
		return failHost(hr, "ensure_dir", err)
	}

	// Step 2: Copy compose files.
	logger.Info().Msg("copying deploy files")
	files := []string{cfg.Source.ComposeFile}
	if err := e.Remote.CopyFiles(ctx, target, input.LocalAppDir, files); err != nil {
		return failHost(hr, "copy_files", err)
	}

	// Step 3: Docker compose pull.
	logger.Info().Msg("pulling images")
	pullCmd := fmt.Sprintf("cd %s && docker compose pull", shellQuote(target.Path))
	if out, err := e.Remote.Run(ctx, target, pullCmd); err != nil {
		hr.Logs = out
		return failHost(hr, "docker_pull", err)
	}

	// Step 4: Docker compose up.
	logger.Info().Msg("starting services")
	upCmd := fmt.Sprintf("cd %s && docker compose up -d --remove-orphans", shellQuote(target.Path))
	if out, err := e.Remote.Run(ctx, target, upCmd); err != nil {
		hr.Logs = out
		return failHost(hr, "docker_up", err)
	}

	// Step 5: Health check.
	if cfg.Deploy.Health.Type != "" && cfg.Deploy.Health.Type != "none" {
		logger.Info().Msg("running health check")
		if err := e.Health.Check(ctx, cfg.Deploy.Health); err != nil {
			return failHost(hr, "health_check", err)
		}
	}

	// Step 6: Write release marker.
	logger.Info().Msg("writing release marker")
	marker := model.ReleaseMarker{
		ReleaseID: release.ID,
		CommitSHA: input.Plan.CommitSHA,
		App:       input.Plan.App,
		Env:       input.Plan.Env,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}
	markerJSON, _ := json.MarshalIndent(marker, "", "  ")
	markerPath := path.Join(target.Path, ".helmforge-release")
	if err := e.Remote.WriteFile(ctx, target, markerPath, string(markerJSON)); err != nil {
		return failHost(hr, "write_marker", err)
	}

	hr.Status = model.ReleaseStatusSuccess
	hr.FinishedAt = time.Now().UTC()
	logger.Info().Msg("host deploy complete")
	return hr
}

func failHost(hr model.HostResult, step string, err error) model.HostResult {
	hr.Status = model.ReleaseStatusFailed
	hr.FinishedAt = time.Now().UTC()
	hr.Error = fmt.Sprintf("step %s: %s", step, err.Error())
	log.L().Error().Str("host", hr.Host).Str("step", step).Err(err).Msg("host deploy failed")
	return hr
}

func generateReleaseID() string {
	return fmt.Sprintf("rel-%d", time.Now().UnixNano())
}

// CheckDrift reads the remote marker file and compares commit SHAs.
func (e *Engine) CheckDrift(ctx context.Context, target model.Target, desiredSHA string) model.DriftResult {
	result := model.DriftResult{
		Host:       target.Host,
		DesiredSHA: desiredSHA,
	}

	markerPath := path.Join(target.Path, ".helmforge-release")
	content, err := e.Remote.ReadFile(ctx, target, markerPath)
	if err != nil {
		result.Error = err.Error()
		return result
	}
	if content == "" {
		result.Error = "no release marker found"
		return result
	}

	var marker model.ReleaseMarker
	if err := json.Unmarshal([]byte(content), &marker); err != nil {
		result.Error = fmt.Sprintf("parse marker: %s", err)
		return result
	}

	result.ActualSHA = marker.CommitSHA
	result.InSync = marker.CommitSHA == desiredSHA
	return result
}

func shellQuote(s string) string {
	return "'" + fmt.Sprintf("%s", replaceAll(s, "'", "'\\''")) + "'"
}

func replaceAll(s, old, new string) string {
	result := ""
	for i := 0; i < len(s); {
		if i+len(old) <= len(s) && s[i:i+len(old)] == old {
			result += new
			i += len(old)
		} else {
			result += string(s[i])
			i++
		}
	}
	return result
}
