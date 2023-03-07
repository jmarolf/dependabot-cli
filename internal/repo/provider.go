package repo

import "github.com/dependabot/cli/internal/model"

type Provider interface {
	GetExistingPRs() [][]model.ExistingPR
	CreatePullRequest(m model.CreatePullRequest) (err error)
}
