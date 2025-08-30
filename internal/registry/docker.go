package registry

import "context"

type DockerConfig struct {
    Registry   string
    Username   string
    Password   string
    RepoPrefix string
}

type dockerClient struct { cfg DockerConfig }

func NewDocker(cfg DockerConfig) (Target, error) { return &dockerClient{cfg: cfg}, nil }
func (d *dockerClient) Registry() string   { return d.cfg.Registry }
func (d *dockerClient) RepoPrefix() string { return d.cfg.RepoPrefix }
func (d *dockerClient) EnsureRepository(ctx context.Context, name string) error { return nil }
func (d *dockerClient) BasicAuth(ctx context.Context) (string, string, error) { return d.cfg.Username, d.cfg.Password, nil }
