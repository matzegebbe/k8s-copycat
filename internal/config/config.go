package config

import (
	"io/fs"
	"os"

	"sigs.k8s.io/yaml"

	"github.com/matzegebbe/k8s-copycat/pkg/util"
)

// FilePath returns default config path inside the container.
const FilePath = "/config/config.yaml"

type ECR struct {
	AccountID  string `yaml:"accountID"`
	Region     string `yaml:"region"`
	RepoPrefix string `yaml:"repoPrefix"`
	CreateRepo *bool  `yaml:"createRepo"`
}

type Docker struct {
	Registry   string `yaml:"registry"`
	RepoPrefix string `yaml:"repoPrefix"`
	Insecure   bool   `yaml:"insecure"`
	// Username/Password should come from Secret envs, not ConfigMap.
}

// RegistryCredential defines credentials for pulling from a registry. Username and
// password can either be set directly or provided via environment variables using
// the *_env fields. When both direct values and env-based overrides are provided,
// the environment variables take precedence at runtime.
type RegistryCredential struct {
	Registry    string `yaml:"registry"`
	Username    string `yaml:"username"`
	Password    string `yaml:"password"`
	UsernameEnv string `yaml:"usernameEnv"`
	PasswordEnv string `yaml:"passwordEnv"`
	Token       string `yaml:"token"`
	TokenEnv    string `yaml:"tokenEnv"`
}

type Config struct {
	TargetKind          string               `yaml:"targetKind"` // ecr | docker
	LogLevel            string               `yaml:"logLevel"`
	ECR                 ECR                  `yaml:"ecr"`
	Docker              Docker               `yaml:"docker"`
	DryRun              bool                 `yaml:"dryRun"`
	RequestTimeout      string               `yaml:"requestTimeout"`
	RegistryCredentials []RegistryCredential `yaml:"registryCredentials"`
	PathMap             []util.PathMapping   `yaml:"pathMap"`
}

func Load(path string) (Config, bool, error) {
	var c Config
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return c, false, nil
		}
		// treat permission-denied as non-fatal not-found
		if perr, ok := err.(*fs.PathError); ok && perr.Err != nil {
			return c, false, nil
		}
		return c, false, err
	}
	if err := yaml.Unmarshal(b, &c); err != nil {
		return c, false, err
	}
	return c, true, nil
}
