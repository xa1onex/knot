package deployrunner

import (
	"strings"
	"testing"

	"github.com/knot-infra/knot/pkg/protocol"
)

func TestDockerRunArgsIsolation(t *testing.T) {
	args := dockerRunArgs(protocol.DeploySpec{
		Name:  "web",
		Image: "nginx:alpine",
		Port:  8080,
		Bind:  "127.0.0.1",
	}, "knot-web-abcd1234")
	joined := strings.Join(args, " ")
	for _, want := range []string{
		"--cap-drop ALL",
		"--security-opt no-new-privileges",
		"--pids-limit 256",
		"--memory 512m",
		"--cpus 1.00",
		"--network bridge",
		"-p 127.0.0.1:8080:8080",
		"--name knot-web-abcd1234",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %v", want, args)
		}
	}
}
