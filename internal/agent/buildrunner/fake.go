package buildrunner

import (
	"context"
	"strings"
	"time"

	"github.com/knot-infra/knot/pkg/protocol"
)

type FakeRunner struct{}

func NewFakeRunner() *FakeRunner { return &FakeRunner{} }

func (f *FakeRunner) Run(ctx context.Context, spec protocol.BuildSpec, emit func(stream, line string), progress func(status string)) protocol.BuildResult {
	res := protocol.BuildResult{BuildID: spec.BuildID, Image: spec.Tag}
	kind := strings.TrimPrefix(spec.URL, "knot-fake-git:")

	progress(protocol.BuildStatusCloning)
	emit("stdout", "cloning "+spec.URL)
	if spec.GitToken != "" {
		emit("stdout", "git auth token="+spec.GitToken)
	}

	switch kind {
	case "private":
		if spec.GitToken == "" {
			res.Status = protocol.BuildStatusFailedClone
			res.Error = "authentication required"
			emit("stderr", res.Error)
			return res
		}
	case "hang":
		select {
		case <-ctx.Done():
			res.Status = protocol.BuildStatusFailed
			res.Error = ctx.Err().Error()
			return res
		case <-time.After(2 * time.Minute):
		}
	}

	progress(protocol.BuildStatusBuilding)
	emit("stdout", "docker build -f "+spec.Dockerfile+" -t "+spec.Tag+" "+spec.Context)
	if kind == "bad-dockerfile" {
		res.Status = protocol.BuildStatusFailedBuild
		res.Error = "dockerfile failed"
		emit("stderr", "ERROR: failed to solve: dockerfile")
		return res
	}

	progress(protocol.BuildStatusPushing)
	emit("stdout", "docker push "+spec.Tag)
	if kind == "fail-push" {
		res.Status = protocol.BuildStatusFailedPush
		res.Error = "push denied"
		emit("stderr", res.Error)
		return res
	}

	res.OK = true
	res.Status = protocol.BuildStatusCompleted
	res.Revision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	if spec.Revision != "" {
		res.Revision = spec.Revision
	}
	emit("stdout", "built "+spec.Tag)
	return res
}
