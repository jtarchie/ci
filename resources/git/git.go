package git

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
	"github.com/go-git/go-git/v5/plumbing/transport"
	"github.com/go-git/go-git/v5/plumbing/transport/http"
	"github.com/go-git/go-git/v5/plumbing/transport/ssh"
	"github.com/jtarchie/ci/resources"
	cryptossh "golang.org/x/crypto/ssh"
)

// Git implements the native git resource.
type Git struct{}

// sourceConfig holds the parsed source configuration for the git resource.
type sourceConfig struct {
	URI           string
	Branch        string
	PrivateKey    string
	Username      string
	Password      string
	Depth         int
	TagFilter     string
	SkipSSLVerify bool
}

func parseSourceConfig(source map[string]any) (*sourceConfig, error) {
	cfg := &sourceConfig{
		Branch: "main", // Default branch
	}

	if uri, ok := source["uri"].(string); ok {
		cfg.URI = uri
	} else {
		return nil, fmt.Errorf("uri is required")
	}

	if branch, ok := source["branch"].(string); ok {
		cfg.Branch = branch
	}

	if pk, ok := source["private_key"].(string); ok {
		cfg.PrivateKey = pk
	}

	if username, ok := source["username"].(string); ok {
		cfg.Username = username
	}

	if password, ok := source["password"].(string); ok {
		cfg.Password = password
	}

	if depth, ok := source["depth"].(float64); ok {
		cfg.Depth = int(depth)
	}

	if tagFilter, ok := source["tag_filter"].(string); ok {
		cfg.TagFilter = tagFilter
	}

	if skipSSL, ok := source["skip_ssl_verify"].(bool); ok {
		cfg.SkipSSLVerify = skipSSL
	}

	return cfg, nil
}

func (cfg *sourceConfig) getAuth() (transport.AuthMethod, error) {
	if cfg.PrivateKey != "" {
		// SSH key authentication
		publicKeys, err := ssh.NewPublicKeys("git", []byte(cfg.PrivateKey), "")
		if err != nil {
			return nil, fmt.Errorf("failed to create ssh auth: %w", err)
		}

		// Allow any host key (in production, you'd want to verify)
		publicKeys.HostKeyCallback = cryptossh.InsecureIgnoreHostKey() //nolint:gosec

		return publicKeys, nil
	}

	if cfg.Username != "" && cfg.Password != "" {
		// HTTP basic auth
		return &http.BasicAuth{
			Username: cfg.Username,
			Password: cfg.Password,
		}, nil
	}

	// No auth (public repository)
	return nil, nil //nolint:nilnil
}

func (g *Git) Name() string {
	return "git"
}

// Check discovers new versions (commits) of the git repository.
func (g *Git) Check(ctx context.Context, req resources.CheckRequest) (resources.CheckResponse, error) {
	cfg, err := parseSourceConfig(req.Source)
	if err != nil {
		return nil, fmt.Errorf("invalid source config: %w", err)
	}

	auth, err := cfg.getAuth()
	if err != nil {
		return nil, fmt.Errorf("failed to get auth: %w", err)
	}

	// Create a temporary directory for the bare clone
	tmpDir, err := os.MkdirTemp("", "git-check-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create temp dir: %w", err)
	}

	defer func() {
		_ = os.RemoveAll(tmpDir)
	}()

	// Use git.Init and fetch to get refs without full clone
	repo, err := git.PlainInit(tmpDir, true)
	if err != nil {
		return nil, fmt.Errorf("failed to init bare repo: %w", err)
	}

	// Create remote
	remote, err := repo.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{cfg.URI},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create remote: %w", err)
	}

	// Fetch the branch
	refSpec := config.RefSpec(fmt.Sprintf("+refs/heads/%s:refs/heads/%s", cfg.Branch, cfg.Branch))

	fetchOptions := &git.FetchOptions{
		RemoteName: "origin",
		RefSpecs:   []config.RefSpec{refSpec},
		Auth:       auth,
	}

	if cfg.Depth > 0 {
		fetchOptions.Depth = cfg.Depth
	}

	err = remote.FetchContext(ctx, fetchOptions)
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return nil, fmt.Errorf("failed to fetch: %w", err)
	}

	// Get the branch reference
	branchRef, err := repo.Reference(plumbing.NewBranchReferenceName(cfg.Branch), true)
	if err != nil {
		return nil, fmt.Errorf("failed to get branch ref: %w", err)
	}

	// Get commit history
	commitIter, err := repo.Log(&git.LogOptions{
		From:  branchRef.Hash(),
		Order: git.LogOrderCommitterTime,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get log: %w", err)
	}

	var commits []resources.Version

	// Collect commits
	err = commitIter.ForEach(func(c *object.Commit) error {
		commits = append(commits, resources.Version{
			"ref": c.Hash.String(),
		})

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to iterate commits: %w", err)
	}

	// If no version specified, return only the latest
	if req.Version == nil || req.Version["ref"] == "" {
		if len(commits) == 0 {
			return resources.CheckResponse{}, nil
		}

		return resources.CheckResponse{commits[0]}, nil
	}

	// Find the requested version and return all versions after it
	requestedRef := req.Version["ref"]
	var result resources.CheckResponse

	foundRequested := false

	for i := len(commits) - 1; i >= 0; i-- {
		if commits[i]["ref"] == requestedRef {
			foundRequested = true
		}

		if foundRequested {
			result = append(result, commits[i])
		}
	}

	// If we didn't find the requested version (e.g., force push), return the latest
	if !foundRequested && len(commits) > 0 {
		return resources.CheckResponse{commits[0]}, nil
	}

	return result, nil
}

// In fetches a specific version of the git repository.
func (g *Git) In(ctx context.Context, destDir string, req resources.InRequest) (resources.InResponse, error) {
	cfg, err := parseSourceConfig(req.Source)
	if err != nil {
		return resources.InResponse{}, fmt.Errorf("invalid source config: %w", err)
	}

	auth, err := cfg.getAuth()
	if err != nil {
		return resources.InResponse{}, fmt.Errorf("failed to get auth: %w", err)
	}

	ref := req.Version["ref"]
	if ref == "" {
		return resources.InResponse{}, fmt.Errorf("version ref is required")
	}

	// Parse params
	depth := cfg.Depth
	if d, ok := req.Params["depth"].(float64); ok {
		depth = int(d)
	}

	submodules := true
	if sm, ok := req.Params["submodules"].(string); ok {
		submodules = sm != "none"
	}

	// Clone the repository
	cloneOptions := &git.CloneOptions{
		URL:           cfg.URI,
		Auth:          auth,
		ReferenceName: plumbing.NewBranchReferenceName(cfg.Branch),
		SingleBranch:  true,
	}

	if depth > 0 {
		cloneOptions.Depth = depth
	}

	repo, err := git.PlainCloneContext(ctx, destDir, false, cloneOptions)
	if err != nil {
		return resources.InResponse{}, fmt.Errorf("failed to clone: %w", err)
	}

	// Checkout the specific commit
	worktree, err := repo.Worktree()
	if err != nil {
		return resources.InResponse{}, fmt.Errorf("failed to get worktree: %w", err)
	}

	err = worktree.Checkout(&git.CheckoutOptions{
		Hash: plumbing.NewHash(ref),
	})
	if err != nil {
		return resources.InResponse{}, fmt.Errorf("failed to checkout: %w", err)
	}

	// Handle submodules
	if submodules {
		submodulesList, err := worktree.Submodules()
		if err == nil {
			for _, sm := range submodulesList {
				_ = sm.UpdateContext(ctx, &git.SubmoduleUpdateOptions{
					Init:              true,
					RecurseSubmodules: git.DefaultSubmoduleRecursionDepth,
					Auth:              auth,
				})
			}
		}
	}

	// Get commit info for metadata
	commit, err := repo.CommitObject(plumbing.NewHash(ref))
	if err != nil {
		return resources.InResponse{}, fmt.Errorf("failed to get commit: %w", err)
	}

	// Write version info files (compatible with Concourse git resource)
	versionFile := filepath.Join(destDir, ".git", "ref")

	err = os.WriteFile(versionFile, []byte(ref), 0o600)
	if err != nil {
		return resources.InResponse{}, fmt.Errorf("failed to write ref file: %w", err)
	}

	shortRef := filepath.Join(destDir, ".git", "short_ref")

	err = os.WriteFile(shortRef, []byte(ref[:8]), 0o600)
	if err != nil {
		return resources.InResponse{}, fmt.Errorf("failed to write short_ref file: %w", err)
	}

	return resources.InResponse{
		Version: resources.Version{
			"ref": ref,
		},
		Metadata: resources.Metadata{
			{Name: "commit", Value: ref},
			{Name: "author", Value: commit.Author.Name},
			{Name: "author_email", Value: commit.Author.Email},
			{Name: "message", Value: strings.Split(commit.Message, "\n")[0]},
			{Name: "committer", Value: commit.Committer.Name},
			{Name: "committer_email", Value: commit.Committer.Email},
		},
	}, nil
}

// Out pushes changes to the git repository.
func (g *Git) Out(ctx context.Context, srcDir string, req resources.OutRequest) (resources.OutResponse, error) {
	cfg, err := parseSourceConfig(req.Source)
	if err != nil {
		return resources.OutResponse{}, fmt.Errorf("invalid source config: %w", err)
	}

	auth, err := cfg.getAuth()
	if err != nil {
		return resources.OutResponse{}, fmt.Errorf("failed to get auth: %w", err)
	}

	// Get repository path from params
	repoPath, ok := req.Params["repository"].(string)
	if !ok {
		return resources.OutResponse{}, fmt.Errorf("repository param is required")
	}

	fullRepoPath := filepath.Join(srcDir, repoPath)

	// Open the existing repository
	repo, err := git.PlainOpen(fullRepoPath)
	if err != nil {
		return resources.OutResponse{}, fmt.Errorf("failed to open repo: %w", err)
	}

	// Get branch to push (default from source config)
	branch := cfg.Branch
	if b, ok := req.Params["branch"].(string); ok {
		branch = b
	}

	// Push to remote
	pushOptions := &git.PushOptions{
		RemoteName: "origin",
		RefSpecs: []config.RefSpec{
			config.RefSpec(fmt.Sprintf("refs/heads/%s:refs/heads/%s", branch, branch)),
		},
		Auth: auth,
	}

	// Handle force push
	if force, ok := req.Params["force"].(bool); ok && force {
		pushOptions.Force = true
	}

	err = repo.PushContext(ctx, pushOptions)
	if err != nil && err != git.NoErrAlreadyUpToDate {
		return resources.OutResponse{}, fmt.Errorf("failed to push: %w", err)
	}

	// Get the HEAD commit
	head, err := repo.Head()
	if err != nil {
		return resources.OutResponse{}, fmt.Errorf("failed to get HEAD: %w", err)
	}

	commit, err := repo.CommitObject(head.Hash())
	if err != nil {
		return resources.OutResponse{}, fmt.Errorf("failed to get commit: %w", err)
	}

	return resources.OutResponse{
		Version: resources.Version{
			"ref": head.Hash().String(),
		},
		Metadata: resources.Metadata{
			{Name: "commit", Value: head.Hash().String()},
			{Name: "author", Value: commit.Author.Name},
			{Name: "message", Value: strings.Split(commit.Message, "\n")[0]},
		},
	}, nil
}

func init() {
	resources.Register("git", func() resources.Resource {
		return &Git{}
	})
}

var _ resources.Resource = &Git{}
