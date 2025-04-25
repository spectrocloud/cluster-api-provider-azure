package scope

import (
	"context"
	"os"

	"github.com/Azure/go-autorest/autorest/azure"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

const (
	AzureEnvironentFileEnvName = "AZURE_ENVIRONMENT_FILE"
	AzureSecretCloudName       = "AzureSecretCloud"
)

func init() {
	_, log, done := tele.StartSpanWithLogger(context.Background(), "scope.AzureScope.init")
	defer done()
	path := os.Getenv(AzureEnvironentFileEnvName)
	if path == "" {
		return
	}
	if env, err := azure.EnvironmentFromFile(path); err == nil {
		azure.SetEnvironment(AzureSecretCloudName, env)
	} else {
		log.Error(err, "reason", "failed to load Azure environment from file", "path", path)
	}
}
