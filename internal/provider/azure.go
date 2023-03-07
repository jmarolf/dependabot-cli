package provider

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/dependabot/cli/internal/model"
)

type azureProvider struct {
	repo  string
	creds []model.Credential
}

// NewAPI creates a new API instance and starts the server
func NewAzureProvider(repo string, credentials []model.Credential) Provider {
	return &azureProvider{
		repo:  repo,
		creds: credentials,
	}
}

// Port returns the port the API is listening on
func (a *azureProvider) CreatePullRequest(m model.CreatePullRequest) (err error) {
	cred := findCredentialForDomain(a.creds, "dev.azure.com") // TODO: Switch the domain based on the provider
	repoParts := strings.Split(a.repo, "/")
	adoOrg := repoParts[0]
	adoProject := repoParts[1]
	adoRepo := repoParts[3]
	dependencyName := m.Dependencies[0].Name
	dependencyVersion := m.Dependencies[0].Version

	// TODO: This branch name should take the directory that dependabot started from into account to avoid branch conflicts.
	branchName := fmt.Sprintf("refs/heads/dependabot/%s/%s", dependencyName, *dependencyVersion)

	var fileChanges []interface{}
	for _, v := range m.UpdatedDependencyFiles {
		fileChanges = append(fileChanges, map[string]interface{}{
			"changeType": "edit",
			"item": map[string]interface{}{
				"path": v.Name,
			},
			"newContent": map[string]interface{}{
				"content":     base64.StdEncoding.EncodeToString([]byte(v.Content)),
				"contentType": "base64encoded",
			},
		})
	}

	branchCreateBody := map[string]interface{}{
		"refUpdates": []interface{}{
			map[string]interface{}{
				"name":        branchName,
				"oldObjectId": m.BaseCommitSha,
			},
		},
		"commits": []interface{}{
			map[string]interface{}{
				"comment": m.CommitMessage,
				"changes": fileChanges,
			},
		},
	}
	createBranchRequestUrl := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pushes?api-version=7.1-preview.2", adoOrg, adoProject, adoRepo)
	_, err = sendHttpRequest("POST", createBranchRequestUrl, cred, branchCreateBody)
	if err != nil {
		return
	}

	prCreateBody := map[string]interface{}{
		"sourceRefName": branchName,
		"targetRefName": "refs/heads/main", // TODO: This needs to detect the default branch
		"title":         m.PRTitle,
		"description":   m.PRBody,
	}
	createPrRequestUrl := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests?api-version=7.1-preview.1", adoOrg, adoProject, adoRepo)
	_, err = sendHttpRequest("POST", createPrRequestUrl, cred, prCreateBody)
	if err != nil {
		return
	}

	return
}

func sendHttpRequest(method string, url string, cred model.Credential, body any) (resp any, err error) {
	client := &http.Client{}
	w := new(bytes.Buffer)
	err = json.NewEncoder(w).Encode(body)
	if err != nil {
		return
	}

	req, err := http.NewRequest(method, url, w)
	if err != nil {
		return
	}

	req.Header.Add("Content-Type", "application/json")

	if cred != nil {
		req.SetBasicAuth(cred["username"].(string), cred["password"].(string))
	}

	response, err := client.Do(req)
	if err != nil {
		resp = response
	}

	// TODO: Remove this code chunk.  It's only for debugging
	log.Println(response.Status)
	var r interface{}
	err = json.NewDecoder(response.Body).Decode(&r)
	log.Println(r)

	if err != nil {
		resp = response
	}

	return
}

func findCredentialForDomain(creds []model.Credential, domain string) model.Credential {
	for _, v := range creds {
		if v["host"].(string) == domain {
			// Make a copy to expand the secret
			var cred = model.Credential{}
			for key, value := range v {
				if valueString, ok := value.(string); ok {
					cred[key] = os.ExpandEnv(valueString)
				}
			}
			return cred
		}
	}

	return nil
}
