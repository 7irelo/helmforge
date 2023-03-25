package validate

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/7irelo/helmforge/internal/core/model"
	"gopkg.in/yaml.v3"
)

// ValidationError holds a list of problems found in config.
type ValidationError struct {
	Errors []FieldError
}

// FieldError is a single validation issue.
type FieldError struct {
	Field   string
	Message string
	Line    int
}

func (ve *ValidationError) Error() string {
	var b strings.Builder
	for _, e := range ve.Errors {
		if e.Line > 0 {
			fmt.Fprintf(&b, "  line %d: %s: %s\n", e.Line, e.Field, e.Message)
		} else {
			fmt.Fprintf(&b, "  %s: %s\n", e.Field, e.Message)
		}
	}
	return b.String()
}

// ParseAndValidate parses raw YAML bytes into an AppConfig and validates it.
func ParseAndValidate(data []byte) (*model.AppConfig, error) {
	var cfg model.AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("yaml parse error: %w", err)
	}

	// Parse into yaml.Node for line number info.
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("yaml node parse error: %w", err)
	}

	errs := validate(&cfg, &doc)
	if len(errs) > 0 {
		return nil, &ValidationError{Errors: errs}
	}

	// Apply defaults.
	for i := range cfg.Targets {
		if cfg.Targets[i].Port == 0 {
			cfg.Targets[i].Port = 22
		}
	}
	if cfg.Deploy.Strategy == "" {
		cfg.Deploy.Strategy = "rolling"
	}
	if cfg.Deploy.Health.TimeoutSeconds == 0 {
		cfg.Deploy.Health.TimeoutSeconds = 30
	}

	return &cfg, nil
}

func validate(cfg *model.AppConfig, doc *yaml.Node) []FieldError {
	var errs []FieldError

	lineOf := buildLineMap(doc)

	if cfg.App == "" {
		errs = append(errs, FieldError{Field: "app", Message: "required", Line: lineOf["app"]})
	}
	if cfg.Env == "" {
		errs = append(errs, FieldError{Field: "env", Message: "required", Line: lineOf["env"]})
	}
	if len(cfg.Targets) == 0 {
		errs = append(errs, FieldError{Field: "targets", Message: "at least one target required", Line: lineOf["targets"]})
	}
	for i, t := range cfg.Targets {
		prefix := fmt.Sprintf("targets[%d]", i)
		if t.Host == "" {
			errs = append(errs, FieldError{Field: prefix + ".host", Message: "required"})
		}
		if t.User == "" {
			errs = append(errs, FieldError{Field: prefix + ".user", Message: "required"})
		}
		if t.Path == "" {
			errs = append(errs, FieldError{Field: prefix + ".path", Message: "required"})
		}
		if t.Port < 0 || t.Port > 65535 {
			errs = append(errs, FieldError{Field: prefix + ".port", Message: "must be 0-65535"})
		}
	}

	if cfg.Source.Type == "" {
		errs = append(errs, FieldError{Field: "source.type", Message: "required", Line: lineOf["source"]})
	} else if cfg.Source.Type != "compose" {
		errs = append(errs, FieldError{Field: "source.type", Message: "must be 'compose'", Line: lineOf["source"]})
	}
	if cfg.Source.ComposeFile == "" {
		errs = append(errs, FieldError{Field: "source.composeFile", Message: "required"})
	}

	validStrategies := map[string]bool{"rolling": true}
	if cfg.Deploy.Strategy != "" && !validStrategies[cfg.Deploy.Strategy] {
		errs = append(errs, FieldError{Field: "deploy.strategy", Message: "unsupported strategy: " + cfg.Deploy.Strategy})
	}

	if cfg.Deploy.Health.Type != "" {
		switch cfg.Deploy.Health.Type {
		case "http":
			if cfg.Deploy.Health.URL == "" {
				errs = append(errs, FieldError{Field: "deploy.health.url", Message: "required when type is http"})
			} else if _, err := url.ParseRequestURI(cfg.Deploy.Health.URL); err != nil {
				errs = append(errs, FieldError{Field: "deploy.health.url", Message: "invalid URL"})
			}
		case "none":
			// ok
		default:
			errs = append(errs, FieldError{Field: "deploy.health.type", Message: "must be 'http' or 'none'"})
		}
	}

	return errs
}

// buildLineMap walks a yaml.Node document and returns a map of top-level keys to their line numbers.
func buildLineMap(doc *yaml.Node) map[string]int {
	m := make(map[string]int)
	if doc == nil {
		return m
	}
	// doc is the document node; its first content is the mapping.
	var root *yaml.Node
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		root = doc.Content[0]
	} else if doc.Kind == yaml.MappingNode {
		root = doc
	} else {
		return m
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i]
		m[key.Value] = key.Line
	}
	return m
}
