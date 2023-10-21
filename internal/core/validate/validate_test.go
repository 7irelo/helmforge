package validate

import (
	"strings"
	"testing"
)

func TestParseAndValidate_Valid(t *testing.T) {
	yaml := `
app: reporting-service
env: staging
targets:
  - host: 10.0.1.10
    user: deploy
    port: 22
    path: /opt/apps/reporting
source:
  type: compose
  composeFile: docker-compose.yaml
deploy:
  strategy: rolling
  health:
    type: http
    url: http://10.0.1.10:8080/health
    timeoutSeconds: 30
policy:
  allowedBranches:
    - main
    - staging
`

	cfg, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.App != "reporting-service" {
		t.Errorf("app = %q, want %q", cfg.App, "reporting-service")
	}
	if cfg.Env != "staging" {
		t.Errorf("env = %q, want %q", cfg.Env, "staging")
	}
	if len(cfg.Targets) != 1 {
		t.Fatalf("targets len = %d, want 1", len(cfg.Targets))
	}
	if cfg.Targets[0].Host != "10.0.1.10" {
		t.Errorf("target host = %q, want %q", cfg.Targets[0].Host, "10.0.1.10")
	}
	if cfg.Targets[0].Port != 22 {
		t.Errorf("target port = %d, want 22", cfg.Targets[0].Port)
	}
	if cfg.Source.Type != "compose" {
		t.Errorf("source type = %q, want %q", cfg.Source.Type, "compose")
	}
	if cfg.Deploy.Strategy != "rolling" {
		t.Errorf("strategy = %q, want %q", cfg.Deploy.Strategy, "rolling")
	}
	if cfg.Deploy.Health.Type != "http" {
		t.Errorf("health type = %q, want %q", cfg.Deploy.Health.Type, "http")
	}
}

func TestParseAndValidate_Defaults(t *testing.T) {
	yaml := `
app: myapp
env: dev
targets:
  - host: server1
    user: deploy
    path: /opt/myapp
source:
  type: compose
  composeFile: docker-compose.yaml
`
	cfg, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Targets[0].Port != 22 {
		t.Errorf("default port = %d, want 22", cfg.Targets[0].Port)
	}
	if cfg.Deploy.Strategy != "rolling" {
		t.Errorf("default strategy = %q, want %q", cfg.Deploy.Strategy, "rolling")
	}
	if cfg.Deploy.Health.TimeoutSeconds != 30 {
		t.Errorf("default timeout = %d, want 30", cfg.Deploy.Health.TimeoutSeconds)
	}
}

func TestParseAndValidate_MissingApp(t *testing.T) {
	yaml := `
env: staging
targets:
  - host: server1
    user: deploy
    path: /opt/app
source:
  type: compose
  composeFile: docker-compose.yaml
`
	_, err := ParseAndValidate([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing app")
	}
	ve, ok := err.(*ValidationError)
	if !ok {
		t.Fatalf("expected ValidationError, got %T", err)
	}
	found := false
	for _, e := range ve.Errors {
		if e.Field == "app" {
			found = true
		}
	}
	if !found {
		t.Error("expected validation error for field 'app'")
	}
}

func TestParseAndValidate_MissingTargets(t *testing.T) {
	yaml := `
app: myapp
env: staging
targets: []
source:
  type: compose
  composeFile: docker-compose.yaml
`
	_, err := ParseAndValidate([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for empty targets")
	}
	if !strings.Contains(err.Error(), "targets") {
		t.Errorf("error should mention targets: %v", err)
	}
}

func TestParseAndValidate_InvalidSourceType(t *testing.T) {
	yaml := `
app: myapp
env: staging
targets:
  - host: server1
    user: deploy
    path: /opt
source:
  type: kubernetes
  composeFile: something.yaml
`
	_, err := ParseAndValidate([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid source type")
	}
	if !strings.Contains(err.Error(), "compose") {
		t.Errorf("error should mention compose: %v", err)
	}
}

func TestParseAndValidate_InvalidHealthURL(t *testing.T) {
	yaml := `
app: myapp
env: staging
targets:
  - host: server1
    user: deploy
    path: /opt
source:
  type: compose
  composeFile: docker-compose.yaml
deploy:
  health:
    type: http
    url: not-a-url
`
	_, err := ParseAndValidate([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for invalid health URL")
	}
	if !strings.Contains(err.Error(), "invalid URL") {
		t.Errorf("error should mention invalid URL: %v", err)
	}
}

func TestParseAndValidate_MissingTargetFields(t *testing.T) {
	yaml := `
app: myapp
env: staging
targets:
  - host: ""
    user: ""
    path: ""
source:
  type: compose
  composeFile: docker-compose.yaml
`
	_, err := ParseAndValidate([]byte(yaml))
	if err == nil {
		t.Fatal("expected error for missing target fields")
	}
	ve := err.(*ValidationError)
	// Should have errors for host, user, path.
	if len(ve.Errors) < 3 {
		t.Errorf("expected at least 3 errors, got %d: %v", len(ve.Errors), err)
	}
}

func TestParseAndValidate_InvalidYAML(t *testing.T) {
	_, err := ParseAndValidate([]byte(`{{{invalid`))
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

func TestParseAndValidate_HealthTypeNone(t *testing.T) {
	yaml := `
app: myapp
env: staging
targets:
  - host: server1
    user: deploy
    path: /opt
source:
  type: compose
  composeFile: docker-compose.yaml
deploy:
  health:
    type: none
`
	cfg, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Deploy.Health.Type != "none" {
		t.Errorf("health type = %q, want %q", cfg.Deploy.Health.Type, "none")
	}
}

func TestParseAndValidate_MultipleTargets(t *testing.T) {
	yaml := `
app: myapp
env: prod
targets:
  - host: prod1.example.com
    user: deploy
    port: 2222
    path: /opt/myapp
  - host: prod2.example.com
    user: deploy
    path: /opt/myapp
source:
  type: compose
  composeFile: docker-compose.yaml
deploy:
  strategy: rolling
  health:
    type: http
    url: http://prod1.example.com:8080/health
    timeoutSeconds: 60
`
	cfg, err := ParseAndValidate([]byte(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cfg.Targets) != 2 {
		t.Fatalf("targets len = %d, want 2", len(cfg.Targets))
	}
	if cfg.Targets[0].Port != 2222 {
		t.Errorf("first target port = %d, want 2222", cfg.Targets[0].Port)
	}
	if cfg.Targets[1].Port != 22 {
		t.Errorf("second target port = %d, want 22 (default)", cfg.Targets[1].Port)
	}
}
