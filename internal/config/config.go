package config

import (
	"fmt"
	"os"

	"github.com/cloudflare/cloudflare-go"
)

var errCredentialsMissing = fmt.Errorf(`error getting cloudflare credentials!
Supported credentials through environment variables:
'CLOUDFLARE_API_TOKEN' for scoped API token (create here: https://dash.cloudflare.com/profile/api-tokens )
or both 'CLOUDFLARE_API_KEY' and 'CLOUDFLARE_API_EMAIL' for global API key.

Additionally you need to set 'CLOUDFLARE_API_USER_SERVICE_KEY' (Origin CA Key, starts with "v1.0-") if you wish to automatically generate Origin CA certificates`)

func NewCloudflareClient(apiKey, apiEmail, apiToken, apiUserServiceKey string) (api *cloudflare.API, err error) {
	if apiKey == "" {
		apiKey = os.Getenv("CLOUDFLARE_API_KEY")
	}

	if apiEmail == "" {
		apiEmail = os.Getenv("CLOUDFLARE_API_EMAIL")
	}

	if apiToken == "" {
		apiToken = os.Getenv("CLOUDFLARE_API_TOKEN")
	}

	if apiUserServiceKey == "" {
		apiUserServiceKey = os.Getenv("CLOUDFLARE_API_USER_SERVICE_KEY")
	}

	switch {
	case apiToken != "":
		api, err = cloudflare.NewWithAPIToken(apiToken)
	case apiKey != "" && apiEmail != "":
		api, err = cloudflare.New(apiKey, apiEmail)
	default:
		return nil, errCredentialsMissing
	}

	if err != nil {
		return nil, fmt.Errorf("error setting up cloudflare API client: %w", err)
	}

	api.APIUserServiceKey = apiUserServiceKey

	return api, nil
}
