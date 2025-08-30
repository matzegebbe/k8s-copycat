package registry

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	awscfg "github.com/aws/aws-sdk-go-v2/config"
	ecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
)

type ECRConfig struct {
	AccountID  string
	Region     string
	RepoPrefix string
	CreateRepo bool
}

type ecrClient struct {
	cfg      ECRConfig
	client   *ecr.Client
	registry string
}

func NewECR(ctx context.Context, cfg ECRConfig) (Target, error) {
	awsCfg, err := awscfg.LoadDefaultConfig(ctx, awscfg.WithRegion(cfg.Region))
	if err != nil {
		return nil, err
	}
	c := ecr.NewFromConfig(awsCfg)
	reg := fmt.Sprintf("%s.dkr.ecr.%s.amazonaws.com", cfg.AccountID, cfg.Region)
	return &ecrClient{cfg: cfg, client: c, registry: reg}, nil
}

func (c *ecrClient) Registry() string   { return c.registry }
func (c *ecrClient) RepoPrefix() string { return c.cfg.RepoPrefix }
func (c *ecrClient) Insecure() bool     { return false }

func (c *ecrClient) EnsureRepository(ctx context.Context, name string) error {
	_, err := c.client.DescribeRepositories(ctx, &ecr.DescribeRepositoriesInput{RepositoryNames: []string{name}})
	if err == nil {
		return nil
	}
	var rnfe *types.RepositoryNotFoundException
	if c.cfg.CreateRepo && (errors.As(err, &rnfe) || strings.Contains(err.Error(), "RepositoryNotFound")) {
		_, err = c.client.CreateRepository(ctx, &ecr.CreateRepositoryInput{RepositoryName: &name})
		return err
	}
	return err
}

func (c *ecrClient) BasicAuth(ctx context.Context) (username, password string, err error) {
	out, err := c.client.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		return "", "", err
	}
	if len(out.AuthorizationData) == 0 {
		return "", "", fmt.Errorf("no ECR auth data")
	}
	tok := out.AuthorizationData[0].AuthorizationToken
	dec, err := base64.StdEncoding.DecodeString(*tok)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(string(dec), ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected token")
	}
	return parts[0], parts[1], nil
}
