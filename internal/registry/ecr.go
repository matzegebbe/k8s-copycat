package registry

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awscfg "github.com/aws/aws-sdk-go-v2/config"
	ecr "github.com/aws/aws-sdk-go-v2/service/ecr"
	"github.com/aws/aws-sdk-go-v2/service/ecr/types"
	ctrl "sigs.k8s.io/controller-runtime"
)

type ECRConfig struct {
	AccountID  string
	Region     string
	RepoPrefix string
	CreateRepo bool
	// LifecyclePolicy contains optional policy JSON applied when repositories are created.
	LifecyclePolicy string
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
	log := ctrl.LoggerFrom(ctx).WithValues("repository", name, "registry", c.registry)

	describeInput := &ecr.DescribeRepositoriesInput{RepositoryNames: []string{name}}
	if c.cfg.AccountID != "" {
		describeInput.RegistryId = aws.String(c.cfg.AccountID)
	}

	if _, err := c.client.DescribeRepositories(ctx, describeInput); err == nil {
		log.V(1).Info("repository already exists")
		return nil
	} else {
		var rnfe *types.RepositoryNotFoundException
		if c.cfg.CreateRepo && (errors.As(err, &rnfe) || strings.Contains(err.Error(), "RepositoryNotFound")) {
			log.Info("creating repository")
			createInput := &ecr.CreateRepositoryInput{RepositoryName: &name}
			if c.cfg.AccountID != "" {
				createInput.RegistryId = aws.String(c.cfg.AccountID)
			}
			if _, createErr := c.client.CreateRepository(ctx, createInput); createErr != nil {
				log.Error(createErr, "failed to create repository")
				return createErr
			}
			log.Info("repository created")
			policy := strings.TrimSpace(c.cfg.LifecyclePolicy)
			if policy != "" {
				putInput := &ecr.PutLifecyclePolicyInput{
					RepositoryName:      aws.String(name),
					LifecyclePolicyText: aws.String(policy),
				}
				if c.cfg.AccountID != "" {
					putInput.RegistryId = aws.String(c.cfg.AccountID)
				}
				if _, putErr := c.client.PutLifecyclePolicy(ctx, putInput); putErr != nil {
					log.Error(putErr, "failed to apply lifecycle policy")
					return putErr
				}
				log.Info("applied lifecycle policy")
			}
			return nil
		}
		log.Error(err, "failed to describe repository")
		return err
	}
}

func (c *ecrClient) BasicAuth(ctx context.Context) (username, password string, err error) {
	log := ctrl.LoggerFrom(ctx).WithValues("registry", c.registry)

	out, err := c.client.GetAuthorizationToken(ctx, &ecr.GetAuthorizationTokenInput{})
	if err != nil {
		log.Error(err, "failed to get authorization token")
		return "", "", err
	}
	if len(out.AuthorizationData) == 0 {
		noDataErr := fmt.Errorf("no ECR auth data")
		log.Error(noDataErr, "received empty authorization data")
		return "", "", noDataErr
	}
	tok := out.AuthorizationData[0].AuthorizationToken
	dec, err := base64.StdEncoding.DecodeString(*tok)
	if err != nil {
		log.Error(err, "failed to decode authorization token")
		return "", "", err
	}
	parts := strings.SplitN(string(dec), ":", 2)
	if len(parts) != 2 {
		unexpectedErr := fmt.Errorf("unexpected token")
		log.Error(unexpectedErr, "authorization token in unexpected format")
		return "", "", unexpectedErr
	}
	return parts[0], parts[1], nil
}
