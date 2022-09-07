package config

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"

	"github.com/cloudflare/cloudflare-go"
)

type WranglerCloudflareAPI struct {
	api *cloudflare.API
}

func NewWranglerCloudflareAPI(api *cloudflare.API) *WranglerCloudflareAPI {
	return &WranglerCloudflareAPI{
		api: api,
	}
}

func (a *WranglerCloudflareAPI) CreatePagesProject(ctx context.Context, name string) (cloudflare.PagesProject, error) {
	uri := fmt.Sprintf("/accounts/%s/pages/projects", a.api.AccountID)
	r := cloudflare.PagesProject{}

	body := map[string]string{
		"name":              name,
		"production_branch": "main",
	}

	res, err := a.api.Raw(ctx, "POST", uri, body, nil)
	if err != nil {
		return r, err
	}

	err = json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}

	return r, nil
}

func (a *WranglerCloudflareAPI) CreatePagesDeployment(ctx context.Context, name string, manifest map[string]string) error {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	part, err := writer.CreateFormField("manifest")
	if err != nil {
		return err
	}

	manifestBytes, err := json.Marshal(manifest)
	if err != nil {
		return err
	}

	_, err = part.Write(manifestBytes)
	if err != nil {
		return err
	}

	err = writer.Close()
	if err != nil {
		return err
	}

	headers := make(http.Header)
	headers.Set("Content-Type", writer.FormDataContentType())

	_, err = a.api.Raw(ctx, "POST",
		fmt.Sprintf("/accounts/%s/pages/projects/%s/deployments", a.api.AccountID, name), body,
		headers,
	)

	return err
}

func (a *WranglerCloudflareAPI) PagesAPI(name string) *WranglerCloudflarePagesAPI {
	return &WranglerCloudflarePagesAPI{
		api:  a.api,
		name: name,
	}
}

type WranglerCloudflarePagesAPI struct {
	api  *cloudflare.API
	name string

	pagesAPI *cloudflare.API
	jwt      string
}

func (a *WranglerCloudflarePagesAPI) fetchJWT(ctx context.Context) (string, error) {
	uri := fmt.Sprintf("/accounts/%s/pages/projects/%s/upload-token", a.api.AccountID, a.name)
	r := make(map[string]string)

	res, err := a.api.Raw(ctx, "GET", uri, nil, nil)
	if err != nil {
		return "", fmt.Errorf("error fetching cloudflare jwt token for pages: %w", err)
	}

	err = json.Unmarshal(res, &r)
	if err != nil {
		return "", err
	}

	return r["jwt"], nil
}

func (a *WranglerCloudflarePagesAPI) rawPagesRequest(ctx context.Context, method, uri string, data interface{}) (json.RawMessage, error) {
	var err error

	if a.jwt == "" {
		a.jwt, err = a.fetchJWT(ctx)
		if err != nil {
			return nil, err
		}
	}

	if a.pagesAPI == nil {
		a.pagesAPI, err = cloudflare.NewWithAPIToken(a.jwt)
		if err != nil {
			return nil, err
		}
	} else {
		a.pagesAPI.APIToken = a.jwt
	}

	ret, err := a.pagesAPI.Raw(ctx, method, uri, data, nil)

	if _, ok := err.(*cloudflare.AuthorizationError); ok {
		a.jwt = ""

		return a.rawPagesRequest(ctx, method, uri, data)
	}

	return ret, err
}

func (a *WranglerCloudflarePagesAPI) MissingHashes(ctx context.Context, hashes []string) ([]string, error) {
	body := map[string][]string{
		"hashes": hashes,
	}

	res, err := a.rawPagesRequest(ctx, "POST", "/pages/assets/check-missing", body)
	if err != nil {
		return nil, err
	}

	var r []string

	err = json.Unmarshal(res, &r)
	if err != nil {
		return r, err
	}

	return r, nil
}

func (a *WranglerCloudflarePagesAPI) UpsertHashes(ctx context.Context, hashes []string) error {
	body := map[string][]string{
		"hashes": hashes,
	}

	_, err := a.rawPagesRequest(ctx, "POST", "/pages/assets/upsert-hashes", body)

	return err
}

func (a *WranglerCloudflarePagesAPI) UploadBucket(ctx context.Context, payload interface{}) error {
	_, err := a.rawPagesRequest(ctx, "POST", "/pages/assets/upload", payload)

	return err
}
