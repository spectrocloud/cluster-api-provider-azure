package scope

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/Azure/go-autorest/autorest/azure"
	"sigs.k8s.io/cluster-api-provider-azure/util/tele"
)

const (
	AzureEnvironentFolderEnvName = "AZURE_ENVIRONMENT_FOLDER"
)

func init() {
	_, log, done := tele.StartSpanWithLogger(context.Background(), "scope.AzureScope.init")
	defer done()
	path := os.Getenv(AzureEnvironentFolderEnvName)
	if path == "" {
		return
	}
	files, err := os.ReadDir(path)
	if err != nil {
		log.Error(err, "error reading folder", "path", path)
		return
	}

	for _, file := range files {
		if !file.IsDir() && strings.EqualFold(filepath.Ext(file.Name()), ".json") {
			if env, err := azure.EnvironmentFromFile(file.Name()); err == nil {
				azure.SetEnvironment(env.Name, env)
			} else {
				log.Error(err, "failed to load Azure environment from file", "filename", file.Name())
			}
		}
	}
}
