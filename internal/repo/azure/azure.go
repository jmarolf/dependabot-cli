package azure

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strings"

	"github.com/dependabot/cli/internal/model"
	"github.com/dependabot/cli/internal/repo"
)

type azureRepo struct {
	packageManger string
	org           string
	project       string
	repo          string
	directory     string
	cred          model.Credential
}

type azureProvider struct {
	repo  azureRepo
	creds []model.Credential
}

// NewAPI creates a new API instance and starts the server
func NewAzureProvider(packageManager string, repo string, directory string, credentials []model.Credential) repo.Provider {
	cred := findCredentialForDomain(credentials, "dev.azure.com") // TODO: Switch the domain based on the provider
	repoParts := strings.Split(repo, "/")
	return &azureProvider{
		repo: azureRepo{
			packageManger: packageManager,
			org:           repoParts[0],
			project:       repoParts[1],
			repo:          repoParts[3],
			directory:     directory,
			cred:          cred,
		},
		creds: credentials,
	}
}

func (a *azureProvider) GetExistingPRs() [][]model.ExistingPR {
	getRefsUrl := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/refs?filter=heads/dependabot/&filterContains=%s&api-version=7.1-preview.1", a.repo.org, a.repo.project, a.repo.repo, a.repo.directory)
	_, resp, err := sendHttpRequestWithResp[refListResponse]("GET", getRefsUrl, a.repo.cred, nil)
	if err != nil {
		return [][]model.ExistingPR{}
	}

	var existingPRs []model.ExistingPR
	for _, v := range resp.Value {
		existingPRs = append(existingPRs, parseDependencyVersion(a.repo.directory, v.Name))
	}

	return [][]model.ExistingPR{
		existingPRs,
	}
}

// Port returns the port the API is listening on
func (a *azureProvider) CreatePullRequest(m model.CreatePullRequest) (err error) {
	// TODO: This branch name should take the directory that dependabot started from into account to avoid branch conflicts.
	branchName := generateBranchName(a.repo, m.Dependencies[0])

	var fileChanges []interface{}
	for _, v := range m.UpdatedDependencyFiles {
		fileChanges = append(fileChanges, map[string]interface{}{
			"changeType": "edit",
			"item": map[string]interface{}{
				"path": v.Directory + "/" + v.Name,
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
	createBranchRequestUrl := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pushes?api-version=7.1-preview.2", a.repo.org, a.repo.project, a.repo.repo)
	_, err = sendHttpRequest("POST", createBranchRequestUrl, a.repo.cred, branchCreateBody)
	if err != nil {
		return
	}

	prCreateBody := map[string]interface{}{
		"sourceRefName": branchName,
		"targetRefName": "refs/heads/main", // TODO: This needs to detect the default branch
		"title":         m.PRTitle,
		"description":   m.PRBody,
	}
	createPrRequestUrl := fmt.Sprintf("https://dev.azure.com/%s/%s/_apis/git/repositories/%s/pullrequests?api-version=7.1-preview.1", a.repo.org, a.repo.project, a.repo.repo)
	_, err = sendHttpRequest("POST", createPrRequestUrl, a.repo.cred, prCreateBody)
	if err != nil {
		return
	}

	return
}

func sendHttpRequest(method string, url string, cred model.Credential, body any) (statusCode int, err error) {
	statusCode, _, err = sendHttpRequestWithResp[interface{}](method, url, cred, body)
	return
}

func sendHttpRequestWithResp[V any](method string, url string, cred model.Credential, body any) (statusCode int, respBody V, err error) {
	client := &http.Client{}

	var req *http.Request
	if body != nil {
		w := new(bytes.Buffer)
		err = json.NewEncoder(w).Encode(body)
		if err != nil {
			return
		}
		req, err = http.NewRequest(method, url, w)
	} else {
		req, err = http.NewRequest(method, url, nil)
	}

	if err != nil {
		return
	}

	req.Header.Add("Content-Type", "application/json")

	if cred != nil {
		req.SetBasicAuth(cred["username"].(string), cred["password"].(string))
	}

	response, err := client.Do(req)
	if err != nil {
		return
	}

	statusCode = response.StatusCode

	log.Println(response.Status)
	err = json.NewDecoder(response.Body).Decode(&respBody)

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

func generateBranchName(r azureRepo, d model.Dependency) string {
	directory := r.directory

	// Ensure that the directory always has a leading and a trailing slash
	if !strings.HasPrefix(directory, "/") {
		directory = "/" + directory
	}
	if !strings.HasSuffix(directory, "/") {
		directory = directory + "/"
	}

	dependencyName := d.Name
	dependencyVersion := *d.Version
	return fmt.Sprintf("refs/heads/dependabot/%s%s%s-%s", r.packageManger, directory, dependencyName, dependencyVersion)
}

func parseDependencyVersion(directory string, refName string) model.ExistingPR {
	var refNameExp = regexp.MustCompile(`\/(?P<name>[^\/-]*)-(?P<version>[^\/]*)$`)
	match := refNameExp.FindStringSubmatch(refName)
	result := make(map[string]string)
	for i, name := range refNameExp.SubexpNames() {
		if i != 0 && name != "" {
			result[name] = match[i]
		}
	}

	return model.ExistingPR{
		DependencyName:    result["name"],
		DependencyVersion: result["version"],
	}
}
