package cf // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/cfattributesprocessor/internal/cf"

import (
	"bytes"
	"context"
	"encoding/gob"
	"fmt"
	"time"

	bigcache "github.com/allegro/bigcache/v3"
	cfclient "github.com/cloudfoundry/go-cfclient/v3/client"
	cfconfig "github.com/cloudfoundry/go-cfclient/v3/config"
	cfresource "github.com/cloudfoundry/go-cfclient/v3/resource"
	"go.uber.org/zap"
)

type CfAuthType string

const (
	// authTypeClientCredentials uses a client ID and client secret to authenticate
	authTypeClientCredentials CfAuthType = "client_credentials"
	// authTypeUserPass uses username and password to authenticate
	authTypeUserPass CfAuthType = "user_pass"
	// authTypeToken uses access token and refresh token to authenticate
	authTypeToken CfAuthType = "token"
	// BigCache config
	// number of shards (must be a power of 2)
	bigcacheShards  = 1024
	bigcacheVerbose = true
	// Interval between removing expired entries (clean up).
	bigcacheCleanWindow = 1 * time.Minute
)

type Client struct {
	ctx      context.Context
	logger   *zap.Logger
	cacheTTL time.Duration
	cache    *bigcache.BigCache
	// CF API
	cf         *cfclient.Client
	endpoint   string
	authType   CfAuthType
	authID     string
	authSecret string
}

// New initializes a new CF Client with caching .
func New(ctx context.Context, logger *zap.Logger, endpoint string, options ...func(*Client)) (*Client, error) {
	var err error
	var cli *Client

	cli = &Client{
		logger:   logger,
		ctx:      ctx,
		endpoint: endpoint,
		cacheTTL: 10 * time.Minute,
	}
	for _, o := range options {
		o(cli)
	}
	cli.cache, err = cli.newCache()
	if err != nil {
		return nil, err
	}
	cli.cf, err = cli.newCFClient()
	if err != nil {
		return nil, err
	}
	return cli, nil
}

func WithUserPassword(userName, password string) func(*Client) {
	return func(c *Client) {
		c.authType = authTypeUserPass
		c.authID = userName
		c.authSecret = password
	}
}

func WithClientCredentials(clientID, clientSecret string) func(*Client) {
	return func(c *Client) {
		c.authType = authTypeClientCredentials
		c.authID = clientID
		c.authSecret = clientSecret
	}
}

func WithToken(token, refreshToken string) func(*Client) {
	return func(c *Client) {
		c.authType = authTypeToken
		c.authID = token
		c.authSecret = refreshToken
	}
}

func WithCacheTTL(cacheTTL time.Duration) func(*Client) {
	return func(c *Client) {
		c.cacheTTL = cacheTTL
	}
}

func (cfCli *Client) newCache() (*bigcache.BigCache, error) {
	config := bigcache.Config{
		Shards:           bigcacheShards,
		LifeWindow:       cfCli.cacheTTL,
		CleanWindow:      bigcacheCleanWindow,
		HardMaxCacheSize: 0,
		Verbose:          bigcacheVerbose,
	}
	cache, err := bigcache.New(cfCli.ctx, config)
	if err != nil {
		err = fmt.Errorf("could not initialize cache: %w", err)
		cfCli.logger.Error(err.Error())
		return nil, err
	}
	return cache, nil
}

func (cfCli *Client) newCFClient() (*cfclient.Client, error) {
	var cfg *cfconfig.Config
	var err error
	switch cfCli.authType {
	case authTypeUserPass:
		cfg, err = cfconfig.New(cfCli.endpoint, cfconfig.UserPassword(cfCli.authID, cfCli.authSecret))
	case authTypeClientCredentials:
		cfg, err = cfconfig.New(cfCli.endpoint, cfconfig.ClientCredentials(cfCli.authID, cfCli.authSecret))
	case authTypeToken:
		cfg, err = cfconfig.New(cfCli.endpoint, cfconfig.Token(cfCli.authID, cfCli.authSecret))
	}
	if err != nil {
		err = fmt.Errorf("could not create connection configuration for Cloud Foundry: %w", err)
		cfCli.logger.Error(err.Error())
		return nil, err
	}
	c, err := cfclient.New(cfg)
	if err != nil {
		err = fmt.Errorf("could not create connection to Cloud Foundry: %w", err)
		cfCli.logger.Error(err.Error())
		return nil, err
	}
	return c, nil
}

func serialize(val interface{}) ([]byte, error) {
	b := new(bytes.Buffer)
	if err := gob.NewEncoder(b).Encode(val); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func deserialize(data []byte, result interface{}) error {
	return gob.NewDecoder(bytes.NewBuffer(data)).Decode(result)
}

func (cfCli *Client) getApp(appID string) (*cfresource.App, error) {
	var cfApp cfresource.App
	objCacheName := "app:" + appID
	if value, err := cfCli.cache.Get(objCacheName); err != nil {
		// Not found in cache
		app, err := cfCli.cf.Applications.Get(cfCli.ctx, appID)
		if err != nil {
			err = fmt.Errorf("could not retrieve App with guid %s from Cloud Foundry API: %w", objCacheName, err)
			return nil, err
		}
		cfApp = *app
		data, err := serialize(app)
		if err != nil {
			err = fmt.Errorf("could not encode App %s object to store in cache: %w", objCacheName, err)
			return nil, err
		}
		cfCli.cache.Set(objCacheName, data)
	} else {
		if err := deserialize(value, &cfApp); err != nil {
			err = fmt.Errorf("could not decode App %s object stored in cache: %w", objCacheName, err)
			return nil, err
		}
	}
	return &cfApp, nil
}

func (cfCli *Client) GetAppMetadata(appID string) (map[string]*string, map[string]*string, error) {
	app, err := cfCli.getApp(appID)
	if err != nil {
		return nil, nil, err
	}
	return app.Metadata.Labels, app.Metadata.Annotations, nil
}

func (cfCli *Client) GetAppName(appID string) (string, error) {
	app, err := cfCli.getApp(appID)
	if err != nil {
		return "", err
	}
	return app.Name, nil
}

func (cfCli *Client) GetAppSpace(appID string) (string, error) {
	app, err := cfCli.getApp(appID)
	if err != nil {
		return "", err
	}
	return app.Relationships.Space.Data.GUID, nil
}

func (cfCli *Client) GetAppState(appID string) (string, error) {
	app, err := cfCli.getApp(appID)
	if err != nil {
		return "", err
	}
	return app.State, nil
}

func (cfCli *Client) GetAppDates(appID string) (string, string, error) {
	app, err := cfCli.getApp(appID)
	if err != nil {
		return "", "", err
	}
	return app.CreatedAt.String(), app.UpdatedAt.String(), nil
}

func (cfCli *Client) GetAppLifecycle(appID string) (string, []string, string, error) {
	app, err := cfCli.getApp(appID)
	if err != nil {
		return "", []string{}, "", err
	}
	return app.Lifecycle.Type, app.Lifecycle.BuildpackData.Buildpacks, app.Lifecycle.BuildpackData.Stack, nil
}

func (cfCli *Client) getSpace(spaceID string) (*cfresource.Space, error) {
	var cfSpace cfresource.Space
	objCacheName := "space:" + spaceID
	if value, err := cfCli.cache.Get(objCacheName); err != nil {
		// Not found in cache
		space, err := cfCli.cf.Spaces.Get(cfCli.ctx, spaceID)
		if err != nil {
			err = fmt.Errorf("could not retrieve Space with guid %s from Cloud Foundry API: %w", objCacheName, err)
			return nil, err
		}
		cfSpace = *space
		data, err := serialize(space)
		if err != nil {
			err = fmt.Errorf("could not encode Space %s object to store in cache: %w", objCacheName, err)
			return nil, err
		}
		cfCli.cache.Set(objCacheName, data)
	} else {
		if err := deserialize(value, &cfSpace); err != nil {
			err = fmt.Errorf("could not decode Space %s object stored in cache: %w", objCacheName, err)
			return nil, err
		}
	}
	return &cfSpace, nil
}

func (cfCli *Client) GetSpaceName(spaceID string) (string, error) {
	space, err := cfCli.getSpace(spaceID)
	if err != nil {
		return "", err
	}
	return space.Name, nil
}

func (cfCli *Client) GetSpaceMetadata(spaceID string) (map[string]*string, map[string]*string, error) {
	space, err := cfCli.getSpace(spaceID)
	if err != nil {
		return nil, nil, err
	}
	return space.Metadata.Labels, space.Metadata.Annotations, nil
}

func (cfCli *Client) GetSpaceOrg(spaceID string) (string, error) {
	space, err := cfCli.getSpace(spaceID)
	if err != nil {
		return "", err
	}
	return space.Relationships.Organization.Data.GUID, nil
}

func (cfCli *Client) getOrg(orgID string) (*cfresource.Organization, error) {
	var cfOrg cfresource.Organization
	objCacheName := "org:" + orgID
	if value, err := cfCli.cache.Get(objCacheName); err != nil {
		// Not found in cache
		org, err := cfCli.cf.Organizations.Get(cfCli.ctx, orgID)
		if err != nil {
			err = fmt.Errorf("could not retrieve Org with guid %s from Cloud Foundry API: %w", objCacheName, err)
			return nil, err
		}
		cfOrg = *org
		data, err := serialize(org)
		if err != nil {
			err = fmt.Errorf("could not encode Org %s object to store in cache: %w", objCacheName, err)
			return nil, err
		}
		cfCli.cache.Set(objCacheName, data)
	} else {
		if err := deserialize(value, &cfOrg); err != nil {
			err = fmt.Errorf("could not decode Org %s object stored in cache: %w", objCacheName, err)
			return nil, err
		}
	}
	return &cfOrg, nil
}

func (cfCli *Client) GetOrgName(orgID string) (string, error) {
	org, err := cfCli.getOrg(orgID)
	if err != nil {
		return "", err
	}
	return org.Name, nil
}

func (cfCli *Client) GetOrgMetadata(orgID string) (map[string]*string, map[string]*string, error) {
	org, err := cfCli.getOrg(orgID)
	if err != nil {
		return nil, nil, err
	}
	return org.Metadata.Labels, org.Metadata.Annotations, nil
}
