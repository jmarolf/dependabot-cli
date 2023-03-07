package provider

import "github.com/dependabot/cli/internal/model"

type Provider interface {
	CreatePullRequest(m model.CreatePullRequest) (err error)
}
