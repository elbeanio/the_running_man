package tracing

import (
	"os/exec"
	"strings"
	"testing"
)

func lookup(env []string, key string) (values []string) {
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok && k == key {
			values = append(values, v)
		}
	}
	return values
}

func TestProcessEnv_SetsEveryVariable(t *testing.T) {
	env := ProcessEnv([]string{"PATH=/usr/bin"}, "http://localhost:4318", "backend")

	want := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT": "http://localhost:4318",
		"OTEL_SERVICE_NAME":           "backend",
		"OTEL_PROPAGATORS":            "tracecontext,baggage",
		"OTEL_EXPORTER_OTLP_PROTOCOL": "http/protobuf",
		"OTEL_RESOURCE_ATTRIBUTES":    "deployment.environment=local",
		"OTEL_TRACES_SAMPLER":         "always_on",
		"OTEL_METRICS_SAMPLER":        "always_on",
		"OTEL_LOGS_SAMPLER":           "always_on",
	}
	for k, v := range want {
		if got := lookup(env, k); len(got) != 1 || got[0] != v {
			t.Errorf("%s = %v, want exactly [%s]", k, got, v)
		}
	}
	if got := lookup(env, "PATH"); len(got) != 1 || got[0] != "/usr/bin" {
		t.Errorf("PATH = %v, the inherited environment was not kept", got)
	}
}

// The previous implementation prepended its variables without removing
// inherited ones, and os/exec keeps the last value of a duplicated key -- so the
// developer's own OTEL_SERVICE_NAME would have won over Running Man's. Checked
// through a real child process, since the precedence rule is exec's, not ours.
func TestProcessEnv_RunningMansValuesWin(t *testing.T) {
	inherited := []string{
		"PATH=/usr/bin:/bin",
		"OTEL_SERVICE_NAME=from-the-shell",
		"OTEL_EXPORTER_OTLP_ENDPOINT=http://somewhere-else:4318",
	}
	env := ProcessEnv(inherited, "http://localhost:4319", "backend")

	cmd := exec.Command("sh", "-c", `printf '%s %s' "$OTEL_SERVICE_NAME" "$OTEL_EXPORTER_OTLP_ENDPOINT"`)
	cmd.Env = env
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("running a child: %v", err)
	}
	if got := string(out); got != "backend http://localhost:4319" {
		t.Errorf("child saw %q, want Running Man's values", got)
	}
}

// Every inherited OTEL_ variable is removed, not just the ones Running Man sets:
// a stray OTEL_EXPORTER_OTLP_HEADERS pointing at a SaaS collector would
// otherwise travel to the local receiver.
func TestProcessEnv_RemovesAllInheritedOTELVariables(t *testing.T) {
	env := ProcessEnv([]string{"OTEL_EXPORTER_OTLP_HEADERS=x-api-key=secret"}, "http://localhost:4318", "s")
	if got := lookup(env, "OTEL_EXPORTER_OTLP_HEADERS"); len(got) != 0 {
		t.Errorf("an inherited OTEL variable survived: %v", got)
	}
}
