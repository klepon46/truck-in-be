package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Structural regression checks, not a replacement for Jenkins controller validation.
func TestJenkinsRestrictsDatabaseAndRuntimeChangesToDEV(t *testing.T) {
	pipeline := readPipeline(t)
	for _, name := range []string{"Apply Database Migrations", "Deploy DEV Environment"} {
		t.Run(name, func(t *testing.T) {
			stage := pipelineStage(t, pipeline, name)
			guard, _, found := strings.Cut(stage, "steps {")
			if !found || !strings.Contains(guard, "when {") || !strings.Contains(guard, "expression { env.ENVIRONMENT == 'dev' }") {
				t.Fatal("database and runtime changes must be guarded as DEV-only before the steps block; PROD is publish-only")
			}
			for _, variable := range []string{"CONSUL_HTTP_ADDR", "CONSUL_CONFIG_KEY", "CONSUL_ALLOW_INSECURE_HTTP"} {
				if !strings.Contains(stage, `--env "`+variable+`=${`+variable+`}"`) {
					t.Errorf("DEV must still forward %s at container creation", variable)
				}
			}
		})
	}
}

func TestJenkinsPreservesPRODPublicationAndManualHandoff(t *testing.T) {
	pipeline := readPipeline(t)
	approval := pipelineStage(t, pipeline, "Approve Production Image")
	if !strings.Contains(approval, "expression { env.ENVIRONMENT == 'prod' }") || !strings.Contains(approval, "input(message:") {
		t.Fatal("production publication must still require manual approval")
	}
	if !strings.Contains(pipelineStage(t, pipeline, "Push Image"), `docker push "${IMAGE_NAME}"`) {
		t.Fatal("Jenkins must still publish the exact tagged image")
	}
	for _, required := range []string{
		`env.CONSUL_CONFIG_KEY = "${targetEnvironment}/be/truckin-be-config"`,
		"Production image published (not deployed): ${env.IMAGE_NAME}",
		"docs/production-portainer.md", "CONSUL_HTTP_ADDR=${env.CONSUL_HTTP_ADDR}",
		"CONSUL_CONFIG_KEY=${env.CONSUL_CONFIG_KEY}", "CONSUL_ALLOW_INSECURE_HTTP=${env.CONSUL_ALLOW_INSECURE_HTTP}",
	} {
		if !strings.Contains(pipeline, required) {
			t.Errorf("missing production handoff contract: %s", required)
		}
	}
}

func readPipeline(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("..", "Jenkinsfile"))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func pipelineStage(t *testing.T, pipeline, name string) string {
	t.Helper()
	_, body, found := strings.Cut(pipeline, "stage('"+name+"') {")
	if !found {
		t.Fatalf("missing pipeline stage: %s", name)
	}
	stage, _, _ := strings.Cut(body, "\n        stage(")
	return stage
}
