package token

import (
	"context"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/cloud"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"
	az "github.com/Azure/go-autorest/autorest/azure"
	"github.com/pkg/errors"
	"sigs.k8s.io/cluster-api-provider-azure/azure"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

const (
	defaultEnvironmentName = "AzurePublicCloud"
)

type AzureClient struct {
	aadToken *azidentity.ClientSecretCredential
}

// newClient creates a new managed cluster client from an authorizer.
func NewClient(auth azure.Authorizer) (*AzureClient, error) {
	aadToken, err := newAzureActiveDirectoryTokenClient(auth.TenantID(),
		auth.ClientID(),
		auth.ClientSecret(),
		auth.CloudEnvironment())
	if err != nil {
		return nil, err
	}
	return &AzureClient{
		aadToken: aadToken,
	}, nil
}

// newAzureActiveDirectoryTokenClient creates a new aad token client from an authorizer.
func newAzureActiveDirectoryTokenClient(tenantId, clientId, clientSecret, envName string) (*azidentity.ClientSecretCredential, error) {
	cliOpts, err := getAzureClientOptions(envName)
	if err != nil {
		return nil, errors.Wrap(err, "error while getting client options")
	}
	clientOptions := &azidentity.ClientSecretCredentialOptions{
		ClientOptions: cliOpts,
	}
	cred, err := azidentity.NewClientSecretCredential(tenantId, clientId, clientSecret, clientOptions)
	if err != nil {
		return nil, errors.Wrap(err, "error while getting az client secret credentials")
	}
	return cred, nil
}

func getAzureClientOptions(environment string) (azcore.ClientOptions, error) {

	if environment == "" {
		environment = defaultEnvironmentName
	}
	env, err := az.EnvironmentFromName(environment)
	if err != nil {
		return azcore.ClientOptions{}, errors.Wrap(err, "error while getting azure env")
	}
	c := cloud.Configuration{
		ActiveDirectoryAuthorityHost: env.ActiveDirectoryEndpoint,
	}
	return azcore.ClientOptions{
		Cloud: c,
	}, nil
}

func (ac *AzureClient) GetAzureActiveDirectoryToken(ctx context.Context, resourceId string) (string, error) {
	ctx, _, done := tele.StartSpanWithLogger(ctx, "aadToken.GetToken")
	defer done()

	spnAccessToken, err := ac.aadToken.GetToken(ctx, policy.TokenRequestOptions{Scopes: []string{resourceId + "/.default"}})
	if err != nil {
		return "", errors.Wrap(err, "failed to get token")
	}
	return spnAccessToken.Token, nil
}
