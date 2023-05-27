package release

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/7irelo/helmforge/internal/core/model"
)

// FormatPlanText renders a deployment plan as human-readable text.
func FormatPlanText(plan *model.DeployPlan) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Deployment Plan\n")
	fmt.Fprintf(&b, "===============\n")
	fmt.Fprintf(&b, "  App:    %s\n", plan.App)
	fmt.Fprintf(&b, "  Env:    %s\n", plan.Env)
	fmt.Fprintf(&b, "  Repo:   %s\n", plan.Repo)
	fmt.Fprintf(&b, "  Ref:    %s\n", plan.Ref)
	fmt.Fprintf(&b, "  Commit: %s\n\n", plan.CommitSHA)

	currentHost := ""
	for _, a := range plan.Actions {
		if a.Host != currentHost {
			currentHost = a.Host
			fmt.Fprintf(&b, "Host: %s\n", currentHost)
			fmt.Fprintf(&b, "  %s\n", strings.Repeat("-", 50))
		}
		prefix := "  "
		switch a.Step {
		case "ensure_dir":
			prefix = "  + "
		case "copy_files":
			prefix = "  ~ "
		case "docker_pull":
			prefix = "  > "
		case "docker_up":
			prefix = "  > "
		case "health_check":
			prefix = "  ? "
		case "write_marker":
			prefix = "  * "
		}
		fmt.Fprintf(&b, "%s[%s] %s\n", prefix, a.Step, a.Description)
		if a.Command != "" {
			fmt.Fprintf(&b, "      cmd: %s\n", a.Command)
		}
	}
	return b.String()
}

// FormatPlanJSON renders a deployment plan as JSON.
func FormatPlanJSON(plan *model.DeployPlan) (string, error) {
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// FormatReleaseText renders a release as human-readable text.
func FormatReleaseText(r *model.Release) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Release: %s\n", r.ID)
	fmt.Fprintf(&b, "  Status:    %s\n", r.Status)
	fmt.Fprintf(&b, "  App:       %s\n", r.App)
	fmt.Fprintf(&b, "  Env:       %s\n", r.Env)
	fmt.Fprintf(&b, "  Repo:      %s\n", r.Repo)
	fmt.Fprintf(&b, "  Ref:       %s\n", r.Ref)
	fmt.Fprintf(&b, "  Commit:    %s\n", r.CommitSHA)
	fmt.Fprintf(&b, "  Started:   %s\n", r.StartedAt.Format("2006-01-02 15:04:05 UTC"))
	if !r.FinishedAt.IsZero() {
		fmt.Fprintf(&b, "  Finished:  %s\n", r.FinishedAt.Format("2006-01-02 15:04:05 UTC"))
		fmt.Fprintf(&b, "  Duration:  %s\n", r.FinishedAt.Sub(r.StartedAt).Round(time.Millisecond))
	}

	if len(r.HostResults) > 0 {
		fmt.Fprintf(&b, "\n  Host Results:\n")
		tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
		fmt.Fprintf(tw, "    HOST\tSTATUS\tERROR\n")
		for _, hr := range r.HostResults {
			errMsg := ""
			if hr.Error != "" {
				errMsg = hr.Error
				if len(errMsg) > 60 {
					errMsg = errMsg[:57] + "..."
				}
			}
			fmt.Fprintf(tw, "    %s\t%s\t%s\n", hr.Host, hr.Status, errMsg)
		}
		tw.Flush()
	}
	return b.String()
}

// FormatDriftText renders drift results.
func FormatDriftText(results []model.DriftResult) string {
	var b strings.Builder
	tw := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintf(tw, "HOST\tSTATUS\tDESIRED\tACTUAL\tERROR\n")
	for _, r := range results {
		status := "OutOfSync"
		if r.InSync {
			status = "InSync"
		}
		if r.Error != "" {
			status = "Error"
		}
		desired := shortSHA(r.DesiredSHA)
		actual := shortSHA(r.ActualSHA)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", r.Host, status, desired, actual, r.Error)
	}
	tw.Flush()
	return b.String()
}

func shortSHA(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
