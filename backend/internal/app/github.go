package app

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const githubJSONLimit = 2 << 20

var (
	ErrGitHubAssetConflict   = errors.New("GitHub 已有同名资产且内容不同，不能覆盖")
	ErrGitHubReleaseConflict = errors.New("GitHub 已有同名发布且不属于此工程版本，不能覆盖")
	githubNamePattern        = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,100}$`)
	githubCommitPattern      = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)
)

type GitHubClient struct {
	BaseURL    string
	HTTPClient *http.Client
	Token      string
}

type GitHubUser struct {
	ID    int64  `json:"id"`
	Login string `json:"login"`
	Name  string `json:"name"`
}

type GitHubRepo struct {
	ID            int64  `json:"id"`
	FullName      string `json:"full_name"`
	Private       bool   `json:"private"`
	DefaultBranch string `json:"default_branch"`
	HTMLURL       string `json:"html_url"`
	Description   string `json:"description"`
	Permissions   struct {
		Push bool `json:"push"`
	} `json:"permissions"`
}

type GitHubRelease struct {
	ID        int64  `json:"id"`
	TagName   string `json:"tag_name"`
	Name      string `json:"name"`
	Body      string `json:"body"`
	UploadURL string `json:"upload_url"`
	HTMLURL   string `json:"html_url"`
}

type GitHubAsset struct {
	ID                 int64  `json:"id"`
	Name               string `json:"name"`
	URL                string `json:"url"`
	BrowserDownloadURL string `json:"browser_download_url"`
	ContentType        string `json:"content_type"`
	Size               int64  `json:"size"`
	Digest             string `json:"digest"`
}

type GitHubError struct {
	Status int
	Rate   bool
}

func (e *GitHubError) Error() string {
	if e.Rate {
		return "GitHub 请求频率受限，请稍后重试"
	}
	switch e.Status {
	case 401:
		return "GitHub 令牌无效或已撤销"
	case 403:
		return "GitHub 令牌没有执行此操作的权限"
	case 404:
		return "GitHub 仓库或资源不存在，或令牌无权访问"
	case 409, 422:
		return "GitHub 资源冲突，请检查仓库和发布状态"
	default:
		return "GitHub 服务请求失败，请稍后重试"
	}
}

func githubStatusError(response *http.Response) error {
	return &GitHubError{Status: response.StatusCode, Rate: response.StatusCode == 429 || (response.StatusCode == 403 && (response.Header.Get("X-RateLimit-Remaining") == "0" || response.Header.Get("Retry-After") != ""))}
}

func (c *GitHubClient) origin() (*url.URL, error) {
	base := c.BaseURL
	if base == "" {
		base = "https://api.github.com"
	}
	parsed, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != "" {
		return nil, errors.New("GitHub API 地址无效")
	}
	production := parsed.Scheme == "https" && parsed.Host == "api.github.com"
	mock := c.HTTPClient != nil && (parsed.Scheme == "http" || parsed.Scheme == "https") && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost" || parsed.Hostname() == "::1")
	if !production && !mock {
		return nil, errors.New("GitHub API 地址无效")
	}
	if c.Token == "" || len(c.Token) > 4096 || strings.ContainsAny(c.Token, "\r\n\x00") {
		return nil, errors.New("GitHub 令牌格式无效")
	}
	return parsed, nil
}

func sameOrigin(a, b *url.URL) bool {
	return a.Scheme == b.Scheme && strings.EqualFold(a.Host, b.Host)
}

func githubDownloadHost(u *url.URL) bool {
	if u.Scheme != "https" || u.User != nil || (u.Port() != "" && u.Port() != "443") {
		return false
	}
	host := strings.ToLower(u.Hostname())
	return host == "github.com" || host == "api.github.com" || host == "uploads.github.com" || host == "githubusercontent.com" || strings.HasSuffix(host, ".githubusercontent.com")
}

func (c *GitHubClient) client(origin *url.URL, binary bool) *http.Client {
	client := http.Client{Timeout: 15 * time.Minute}
	if c.HTTPClient != nil {
		client = *c.HTTPClient
		if client.Timeout == 0 {
			client.Timeout = 15 * time.Minute
		}
	}
	client.CheckRedirect = func(request *http.Request, previous []*http.Request) error {
		if len(previous) >= 5 || request.URL.User != nil {
			return errors.New("GitHub 文件跳转无效")
		}
		if sameOrigin(origin, request.URL) {
			return nil
		}
		// Downloads may redirect to GitHub's asset CDN, which never receives the PAT.
		request.Header.Del("Authorization")
		request.Header.Del("Cookie")
		if !binary || !githubDownloadHost(request.URL) || request.Method != http.MethodGet {
			return errors.New("GitHub 文件跳转无效")
		}
		return nil
	}
	return &client
}

func (c *GitHubClient) request(ctx context.Context, method, address string, body io.Reader, size int64, contentType string, binary bool) (*http.Response, error) {
	origin, err := c.origin()
	if err != nil {
		return nil, err
	}
	request, err := http.NewRequestWithContext(ctx, method, address, body)
	if err != nil {
		return nil, errors.New("GitHub 请求参数无效")
	}
	request.Header.Set("Authorization", "Bearer "+c.Token)
	request.Header.Set("Accept", "application/vnd.github+json")
	request.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	request.Header.Set("User-Agent", "Project-Hub-Backup")
	if binary {
		request.Header.Set("Accept", "application/octet-stream")
	}
	if body != nil {
		request.ContentLength = size
		request.Header.Set("Content-Type", contentType)
	}
	response, err := c.client(origin, binary).Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("GitHub 请求已取消或超时")
		}
		return nil, errors.New("GitHub 网络请求失败，请稍后重试")
	}
	return response, nil
}

func (c *GitHubClient) address(path string) (string, error) {
	origin, err := c.origin()
	if err != nil {
		return "", err
	}
	return origin.String() + path, nil
}

func (c *GitHubClient) json(ctx context.Context, method, path string, input, output any, status int) error {
	address, err := c.address(path)
	if err != nil {
		return err
	}
	var body io.Reader
	var size int64
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return errors.New("GitHub 请求参数无效")
		}
		body, size = strings.NewReader(string(raw)), int64(len(raw))
	}
	response, err := c.request(ctx, method, address, body, size, "application/json", false)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != status {
		return githubStatusError(response)
	}
	if output == nil {
		return nil
	}
	return decodeGitHubJSON(response.Body, output)
}

func decodeGitHubJSON(reader io.Reader, destination any) error {
	raw, err := io.ReadAll(io.LimitReader(reader, githubJSONLimit+1))
	if err != nil || len(raw) > githubJSONLimit || json.Unmarshal(raw, destination) != nil {
		return errors.New("GitHub 返回数据无效或过大")
	}
	return nil
}

func githubRepoPath(owner, repo string) (string, error) {
	if !githubNamePattern.MatchString(owner) || !githubNamePattern.MatchString(repo) || owner == "." || owner == ".." || repo == "." || repo == ".." {
		return "", errors.New("GitHub 仓库名称无效")
	}
	return "/repos/" + owner + "/" + repo, nil
}

func (c *GitHubClient) User(ctx context.Context) (GitHubUser, error) {
	var user GitHubUser
	err := c.json(ctx, http.MethodGet, "/user", nil, &user, 200)
	if err == nil && (user.ID < 1 || !githubNamePattern.MatchString(user.Login)) {
		err = errors.New("GitHub 账户信息无效")
	}
	return user, err
}

func (c *GitHubClient) Repo(ctx context.Context, owner, repo string) (GitHubRepo, error) {
	var repository GitHubRepo
	path, err := githubRepoPath(owner, repo)
	if err == nil {
		err = c.json(ctx, http.MethodGet, path, nil, &repository, 200)
	}
	if err == nil && (repository.ID < 1 || !strings.EqualFold(repository.FullName, owner+"/"+repo)) {
		err = errors.New("GitHub 仓库信息无效")
	}
	return repository, err
}

func (c *GitHubClient) HeadCommit(ctx context.Context, owner, repo, branch string) (string, error) {
	path, err := githubRepoPath(owner, repo)
	if err != nil {
		return "", err
	}
	if branch == "" || len(branch) > 255 || strings.ContainsAny(branch, "\r\n\x00") {
		return "", errors.New("GitHub 仓库尚未初始化或默认分支无效")
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	err = c.json(ctx, http.MethodGet, path+"/commits/"+url.PathEscape(branch), nil, &commit, 200)
	if err == nil && !githubCommitPattern.MatchString(commit.SHA) {
		err = errors.New("GitHub 默认分支提交无效")
	}
	return commit.SHA, err
}

func (c *GitHubClient) VerifyRepository(ctx context.Context, owner, repo string) (GitHubRepo, string, error) {
	repository, err := c.Repo(ctx, owner, repo)
	if err != nil {
		return repository, "", err
	}
	if !repository.Private {
		return repository, "", errors.New("备份目标必须是私有 GitHub 仓库")
	}
	if !repository.Permissions.Push {
		return repository, "", errors.New("GitHub 令牌需要目标仓库写入权限")
	}
	commit, err := c.HeadCommit(ctx, owner, repo, repository.DefaultBranch)
	return repository, commit, err
}

var backupSlugPattern = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{1,46})[a-z0-9]$`)

func managedBackupRepoName(slug, engineeringID string) (string, error) {
	if !backupSlugPattern.MatchString(slug) || strings.Contains(slug, "--") || !validID(engineeringID) {
		return "", errors.New("灾备仓库名须为 3 至 48 位小写字母、数字或单个连字符，首尾为字母或数字")
	}
	return "ph-" + slug + "-" + strings.ReplaceAll(engineeringID, "-", ""), nil
}

func managedBackupDescription(engineeringID string) string {
	return "Project Hub backup; engineering_id=" + engineeringID
}

// Only managed targets may create repositories. A matching ownership marker is
// required on retries so an existing unrelated repository is never adopted.
func (c *GitHubClient) EnsureManagedRepository(ctx context.Context, target BackupTarget) (string, error) {
	if !target.Managed || !validID(target.EngineeringID) {
		return "", errors.New("自动建仓目标无效")
	}
	description := managedBackupDescription(target.EngineeringID)
	repository, err := c.Repo(ctx, target.Owner, target.Repo)
	var remoteErr *GitHubError
	if errors.As(err, &remoteErr) && remoteErr.Status == 404 {
		user, userErr := c.User(ctx)
		if userErr != nil {
			return "", userErr
		}
		if !strings.EqualFold(user.Login, target.Owner) {
			return "", errors.New("自动建仓账号与仓库所有者不一致，请更新原账号凭证")
		}
		err = c.json(ctx, http.MethodPost, "/user/repos", map[string]any{
			"name": target.Repo, "private": true, "auto_init": true, "description": description,
		}, nil, 201)
		if err != nil {
			return "", fmt.Errorf("自动创建私有灾备仓库失败（令牌须有建仓及 Contents 读写权限）：%w", err)
		}
		repository, err = c.Repo(ctx, target.Owner, target.Repo)
	}
	if err != nil {
		return "", err
	}
	if repository.Description != description {
		return "", errors.New("同名灾备仓库不属于此工程，拒绝上传；请检查仓库归属标记")
	}
	if !repository.Private {
		return "", errors.New("备份目标必须是私有 GitHub 仓库")
	}
	if !repository.Permissions.Push {
		return "", errors.New("GitHub 令牌需要目标仓库写入权限")
	}
	return c.HeadCommit(ctx, target.Owner, target.Repo, repository.DefaultBranch)
}

func backupReleaseBody(engineeringID, versionID string) string {
	return "Project Hub backup\nengineering_id=" + engineeringID + "\nversion_id=" + versionID
}

func ownedBackupRelease(release GitHubRelease) bool {
	lines := strings.Split(release.Body, "\n")
	return release.ID > 0 && len(lines) == 3 && lines[0] == "Project Hub backup" && strings.HasPrefix(lines[1], "engineering_id=") && validID(strings.TrimPrefix(lines[1], "engineering_id=")) && strings.HasPrefix(lines[2], "version_id=") && validID(strings.TrimPrefix(lines[2], "version_id="))
}

func (c *GitHubClient) EnsureRelease(ctx context.Context, owner, repo, tag, name, commit, engineeringID, versionID string) (GitHubRelease, error) {
	var release GitHubRelease
	path, err := githubRepoPath(owner, repo)
	prefix, suffix := "ph-"+engineeringID+"-v", "-"+versionID
	number := strings.TrimSuffix(strings.TrimPrefix(tag, prefix), suffix)
	versionNumber, numberErr := strconv.Atoi(number)
	if err != nil || !validID(engineeringID) || !validID(versionID) || !strings.HasPrefix(tag, prefix) || !strings.HasSuffix(tag, suffix) || numberErr != nil || versionNumber < 1 || !githubCommitPattern.MatchString(commit) || strings.TrimSpace(name) == "" || len(name) > 240 {
		return release, errors.New("GitHub 发布参数无效")
	}
	body := backupReleaseBody(engineeringID, versionID)
	get := func() error {
		return c.json(ctx, http.MethodGet, path+"/releases/tags/"+url.PathEscape(tag), nil, &release, 200)
	}
	err = get()
	var upstream *GitHubError
	if errors.As(err, &upstream) && upstream.Status == 404 {
		err = c.json(ctx, http.MethodPost, path+"/releases", map[string]any{"tag_name": tag, "name": name, "target_commitish": commit, "body": body, "draft": false, "prerelease": false, "make_latest": "false"}, &release, 201)
		if errors.As(err, &upstream) && upstream.Status == 422 {
			err = get()
		}
	}
	if err != nil {
		return release, err
	}
	if release.ID < 1 || release.TagName != tag || release.Body != body {
		return GitHubRelease{}, ErrGitHubReleaseConflict
	}
	return release, nil
}

func (c *GitHubClient) verifyOwnedRelease(ctx context.Context, owner, repo string, release GitHubRelease) error {
	if !ownedBackupRelease(release) {
		return ErrGitHubReleaseConflict
	}
	path, err := githubRepoPath(owner, repo)
	if err != nil {
		return err
	}
	var current GitHubRelease
	if err = c.json(ctx, http.MethodGet, path+"/releases/"+strconv.FormatInt(release.ID, 10), nil, &current, 200); err != nil {
		return err
	}
	if current.ID != release.ID || current.Body != release.Body || current.TagName != release.TagName {
		return ErrGitHubReleaseConflict
	}
	return nil
}

func (c *GitHubClient) ListAssets(ctx context.Context, owner, repo string, releaseID int64) ([]GitHubAsset, error) {
	path, err := githubRepoPath(owner, repo)
	if err != nil || releaseID < 1 {
		return nil, errors.New("GitHub 发布标识无效")
	}
	assets := []GitHubAsset{}
	for page := 1; page <= 10; page++ {
		var batch []GitHubAsset
		if err := c.json(ctx, http.MethodGet, fmt.Sprintf("%s/releases/%d/assets?per_page=100&page=%d", path, releaseID, page), nil, &batch, 200); err != nil {
			return nil, err
		}
		for _, asset := range batch {
			if asset.ID < 1 || asset.Name == "" || asset.Size < 0 || asset.Size > maxZIPBytes {
				return nil, errors.New("GitHub 资产信息无效")
			}
		}
		assets = append(assets, batch...)
		if len(batch) < 100 {
			return assets, nil
		}
	}
	return nil, errors.New("GitHub 发布资产数量过多")
}

func (c *GitHubClient) uploadAddress(owner, repo string, release GitHubRelease, name string) (string, error) {
	origin, err := c.origin()
	if err != nil {
		return "", err
	}
	path, err := githubRepoPath(owner, repo)
	if err != nil {
		return "", err
	}
	address := release.UploadURL
	if i := strings.IndexByte(address, '{'); i >= 0 {
		if address[i:] != "{?name,label}" {
			return "", errors.New("GitHub 上传地址无效")
		}
		address = address[:i]
	}
	parsed, err := url.Parse(address)
	production := err == nil && parsed.Scheme == "https" && parsed.Host == "uploads.github.com"
	mock := err == nil && sameOrigin(origin, parsed) && origin.Host != "api.github.com"
	if err != nil || (!production && !mock) || parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.Path != fmt.Sprintf("%s/releases/%d/assets", path, release.ID) {
		return "", errors.New("GitHub 上传地址无效")
	}
	parsed.RawQuery = url.Values{"name": []string{name}}.Encode()
	return parsed.String(), nil
}

func (c *GitHubClient) UploadAsset(ctx context.Context, owner, repo string, release GitHubRelease, name, contentType string, source io.Reader, size int64) (GitHubAsset, error) {
	var asset GitHubAsset
	if name == "" || len(name) > 200 || strings.ContainsAny(name, "/\\\r\n\x00") || source == nil || size < 1 || size > maxZIPBytes {
		return asset, errors.New("GitHub 上传文件参数无效")
	}
	switch contentType {
	case "application/zip", "application/json", "application/octet-stream", "image/png", "image/jpeg", "image/webp", "image/gif":
	default:
		return asset, errors.New("GitHub 上传文件类型无效")
	}
	address, err := c.uploadAddress(owner, repo, release, name)
	if err != nil {
		return asset, err
	}
	if err = c.verifyOwnedRelease(ctx, owner, repo, release); err != nil {
		return asset, err
	}
	response, err := c.request(ctx, http.MethodPost, address, source, size, contentType, false)
	if err != nil {
		return asset, err
	}
	defer response.Body.Close()
	if response.StatusCode != 201 {
		return asset, githubStatusError(response)
	}
	if err = decodeGitHubJSON(response.Body, &asset); err == nil && (asset.ID < 1 || asset.Name != name || asset.Size != size) {
		err = errors.New("GitHub 上传文件返回信息不一致")
	}
	return asset, err
}

func (c *GitHubClient) DownloadAsset(ctx context.Context, owner, repo string, assetID int64, destination io.Writer) (int64, error) {
	path, err := githubRepoPath(owner, repo)
	if err != nil || assetID < 1 || destination == nil {
		return 0, errors.New("GitHub 下载文件参数无效")
	}
	address, err := c.address(fmt.Sprintf("%s/releases/assets/%d", path, assetID))
	if err != nil {
		return 0, err
	}
	response, err := c.request(ctx, http.MethodGet, address, nil, 0, "", true)
	if err != nil {
		return 0, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return 0, githubStatusError(response)
	}
	if response.ContentLength > maxZIPBytes {
		return 0, errors.New("GitHub 文件超过 256 MB 限制")
	}
	n, err := io.Copy(destination, io.LimitReader(response.Body, maxZIPBytes+1))
	if err != nil {
		return n, errors.New("GitHub 文件下载未完成")
	}
	if n > maxZIPBytes {
		return n, errors.New("GitHub 文件超过 256 MB 限制")
	}
	return n, nil
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func (c *GitHubClient) AssetMatches(ctx context.Context, owner, repo string, asset GitHubAsset, expectedSHA string) (bool, error) {
	if !validSHA256(expectedSHA) {
		return false, errors.New("备份文件校验值无效")
	}
	hash := sha256.New()
	n, err := c.DownloadAsset(ctx, owner, repo, asset.ID, hash)
	return err == nil && n == asset.Size && strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA), err
}

func (c *GitHubClient) EnsureAsset(ctx context.Context, owner, repo string, release GitHubRelease, name, contentType string, source io.ReadSeeker, size int64, expectedSHA string) (GitHubAsset, error) {
	if source == nil || size < 1 || size > maxZIPBytes || !validSHA256(expectedSHA) {
		return GitHubAsset{}, errors.New("备份文件参数无效")
	}
	if err := c.verifyOwnedRelease(ctx, owner, repo, release); err != nil {
		return GitHubAsset{}, err
	}
	find := func() (GitHubAsset, bool, error) {
		assets, err := c.ListAssets(ctx, owner, repo, release.ID)
		if err != nil {
			return GitHubAsset{}, false, err
		}
		var match GitHubAsset
		for _, asset := range assets {
			if asset.Name == name {
				if match.ID != 0 {
					return GitHubAsset{}, false, ErrGitHubAssetConflict
				}
				match = asset
			}
		}
		if match.ID == 0 {
			return match, false, nil
		}
		valid, err := c.AssetMatches(ctx, owner, repo, match, expectedSHA)
		if err == nil && (!valid || match.Size != size) {
			err = ErrGitHubAssetConflict
		}
		return match, true, err
	}
	if asset, found, err := find(); found || err != nil {
		return asset, err
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		return GitHubAsset{}, errors.New("备份文件无法读取")
	}
	hash := sha256.New()
	n, err := io.Copy(hash, io.LimitReader(source, size+1))
	if err != nil || n != size || !strings.EqualFold(hex.EncodeToString(hash.Sum(nil)), expectedSHA) {
		return GitHubAsset{}, errors.New("本地备份文件校验失败")
	}
	if _, err = source.Seek(0, io.SeekStart); err != nil {
		return GitHubAsset{}, errors.New("备份文件无法读取")
	}
	asset, err := c.UploadAsset(ctx, owner, repo, release, name, contentType, source, size)
	var upstream *GitHubError
	if errors.As(err, &upstream) && upstream.Status == 422 {
		if existing, found, findErr := find(); found || findErr != nil {
			return existing, findErr
		}
	}
	if err != nil {
		return asset, err
	}
	valid, err := c.AssetMatches(ctx, owner, repo, asset, expectedSHA)
	if err == nil && !valid {
		err = ErrGitHubAssetConflict
	}
	return asset, err
}

func (c *GitHubClient) DeleteAsset(ctx context.Context, owner, repo string, release GitHubRelease, asset GitHubAsset) error {
	if err := c.verifyOwnedRelease(ctx, owner, repo, release); err != nil {
		return err
	}
	assets, err := c.ListAssets(ctx, owner, repo, release.ID)
	if err != nil {
		return err
	}
	found := false
	for _, current := range assets {
		if current.ID == asset.ID && current.Name == asset.Name {
			found = true
			break
		}
	}
	if !found {
		return errors.New("GitHub 资产不属于此备份发布")
	}
	path, err := githubRepoPath(owner, repo)
	if err != nil {
		return err
	}
	return c.json(ctx, http.MethodDelete, fmt.Sprintf("%s/releases/assets/%d", path, asset.ID), nil, nil, 204)
}

func ParseBackupKey(value string) ([]byte, error) {
	if value == "" {
		return nil, nil
	}
	key, err := base64.StdEncoding.DecodeString(value)
	if err != nil || len(key) != 32 {
		return nil, errors.New("BACKUP_MASTER_KEY 必须是 32 字节密钥的标准 base64 编码")
	}
	return key, nil
}

func backupCipher(key []byte, accountID string) (cipher.AEAD, error) {
	if len(key) != 32 || !validID(accountID) {
		return nil, errors.New("备份加密密钥或账户标识无效")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, errors.New("备份加密配置无效")
	}
	return cipher.NewGCM(block)
}

func encryptBackupToken(key []byte, accountID, token string) (string, error) {
	aead, err := backupCipher(key, accountID)
	if err != nil {
		return "", err
	}
	if token == "" || len(token) > 4096 || strings.ContainsAny(token, "\r\n\x00") {
		return "", errors.New("GitHub 令牌格式无效")
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return "", errors.New("备份令牌加密失败")
	}
	sealed := aead.Seal(nonce, nonce, []byte(token), []byte(accountID))
	return "v1:" + base64.RawStdEncoding.EncodeToString(sealed), nil
}

func decryptBackupToken(key []byte, accountID, encoded string) (string, error) {
	aead, err := backupCipher(key, accountID)
	if err != nil {
		return "", err
	}
	if !strings.HasPrefix(encoded, "v1:") || len(encoded) > 6000 {
		return "", errors.New("备份令牌无法解密，请重新保存令牌")
	}
	raw, err := base64.RawStdEncoding.DecodeString(strings.TrimPrefix(encoded, "v1:"))
	if err != nil || len(raw) < aead.NonceSize()+aead.Overhead() {
		return "", errors.New("备份令牌无法解密，请重新保存令牌")
	}
	token, err := aead.Open(nil, raw[:aead.NonceSize()], raw[aead.NonceSize():], []byte(accountID))
	if err != nil {
		return "", errors.New("备份令牌无法解密，请重新保存令牌")
	}
	return string(token), nil
}

func maskBackupToken(token string) string {
	if len(token) <= 4 {
		return "****"
	}
	return token[len(token)-4:]
}
