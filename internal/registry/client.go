package registry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

type Client struct {
	BaseURL string
	Token   string
	HTTP    *http.Client
}

func New(baseURL, token string) *Client {
	return &Client{
		BaseURL: baseURL,
		Token:   token,
		HTTP:    &http.Client{Timeout: 60 * time.Second},
	}
}

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
}

type AuthResponse struct {
	User  User   `json:"user"`
	Token string `json:"token"`
}

type Asset struct {
	Platform  string `json:"platform"`
	Integrity string `json:"integrity"`
	SizeBytes int64  `json:"size_bytes"`
}

type Version struct {
	Name         string            `json:"name"`
	Version      string            `json:"version"`
	Kind         string            `json:"kind"`
	Dependencies map[string]string `json:"dependencies"`
	PublishedAt  time.Time         `json:"published_at"`
	Assets       []Asset           `json:"assets"`
}

type PackageInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Owner       string `json:"owner"`
	Versions    []struct {
		Version     string    `json:"version"`
		Kind        string    `json:"kind"`
		PublishedAt time.Time `json:"published_at"`
	} `json:"versions"`
}

type Token struct {
	ID        string     `json:"id"`
	Label     string     `json:"label,omitempty"`
	CreatedAt time.Time  `json:"created_at"`
	LastUsed  *time.Time `json:"last_used,omitempty"`
	Token     string     `json:"token,omitempty"`
}

func (c *Client) doJSON(method, path string, body, out interface{}) error {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.BaseURL+path, buf)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return readError(res)
	}
	if out == nil || res.StatusCode == http.StatusNoContent {
		return nil
	}
	return json.NewDecoder(res.Body).Decode(out)
}

func readError(res *http.Response) error {
	b, _ := io.ReadAll(res.Body)
	var ej struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(b, &ej) == nil && ej.Error != "" {
		return fmt.Errorf("%s: %s", res.Status, ej.Error)
	}
	return fmt.Errorf("%s: %s", res.Status, string(b))
}

func (c *Client) Signup(username, email, password string) (*AuthResponse, error) {
	var out AuthResponse
	body := map[string]string{"username": username, "email": email, "password": password}
	if err := c.doJSON("POST", "/v1/auth/signup", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Login(usernameOrEmail, password string) (*AuthResponse, error) {
	var out AuthResponse
	body := map[string]string{"username_or_email": usernameOrEmail, "password": password}
	if err := c.doJSON("POST", "/v1/auth/login", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) Me() (*User, error) {
	var out struct {
		User User `json:"user"`
	}
	if err := c.doJSON("GET", "/v1/me", nil, &out); err != nil {
		return nil, err
	}
	return &out.User, nil
}

func (c *Client) CreateToken(label string) (*Token, error) {
	var out Token
	body := map[string]interface{}{"label": label}
	if err := c.doJSON("POST", "/v1/tokens", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListTokens() ([]Token, error) {
	var out struct {
		Tokens []Token `json:"tokens"`
	}
	if err := c.doJSON("GET", "/v1/tokens", nil, &out); err != nil {
		return nil, err
	}
	return out.Tokens, nil
}

func (c *Client) RevokeToken(id string) error {
	return c.doJSON("DELETE", "/v1/tokens/"+id, nil, nil)
}

func (c *Client) GetPackage(name string) (*PackageInfo, error) {
	var out PackageInfo
	if err := c.doJSON("GET", "/v1/p/"+name, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetVersion(name, version string) (*Version, error) {
	var out Version
	if err := c.doJSON("GET", "/v1/p/"+name+"/"+version, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DownloadAsset(name, version, platform, dst string) (int64, error) {
	url := fmt.Sprintf("%s/v1/p/%s/%s/asset/%s", c.BaseURL, name, version, platform)
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return 0, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return 0, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return 0, readError(res)
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return 0, err
	}
	out, err := os.Create(dst)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	return io.Copy(out, res.Body)
}

type PublishMetadata struct {
	Kind         string            `json:"kind"`
	Dependencies map[string]string `json:"dependencies"`
	Description  string            `json:"description,omitempty"`
	License      string            `json:"license,omitempty"`
	Homepage     string            `json:"homepage,omitempty"`
	Repository   string            `json:"repository,omitempty"`
}

type PublishResponse struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Platform  string `json:"platform"`
	Integrity string `json:"integrity"`
	SizeBytes int64  `json:"size_bytes"`
}

func (c *Client) Publish(name, version, platform, tarballPath string, meta PublishMetadata) (*PublishResponse, error) {
	mb, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		defer pw.Close()
		defer mw.Close()
		_ = mw.WriteField("metadata", string(mb))
		_ = mw.WriteField("platform", platform)
		fw, err := mw.CreateFormFile("asset", filepath.Base(tarballPath))
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		f, err := os.Open(tarballPath)
		if err != nil {
			pw.CloseWithError(err)
			return
		}
		defer f.Close()
		if _, err := io.Copy(fw, f); err != nil {
			pw.CloseWithError(err)
			return
		}
	}()

	url := fmt.Sprintf("%s/v1/p/%s/%s", c.BaseURL, name, version)
	req, err := http.NewRequest("POST", url, pr)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}

	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 {
		return nil, readError(res)
	}
	var out PublishResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}
