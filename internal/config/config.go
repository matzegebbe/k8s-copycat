package config

import (
	"io/fs"
	"os"

	"sigs.k8s.io/yaml"
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
	// Username/Password should come from Secret envs, not ConfigMap.
}

type Config struct {
	TargetKind  string `yaml:"targetKind"` // ecr | docker
	ECR         ECR    `yaml:"ecr"`
	Docker      Docker `yaml:"docker"`
	DryRun      bool   `yaml:"dryRun"`
	StartupPush *bool  `yaml:"startupPush"`
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
