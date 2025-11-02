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
	// Number of cache shards (must be a power of 2)
	// 2048 shards for optimal concurrency with 5000 apps
	bigcacheShards = 2048
	// Verbose logging for cache operations
	bigcacheVerbose = false
	// Max entries in the allocation map before it gets reallocated
	// With 12-hour TTL and frequent deployments:
	// - 5000 apps (relatively stable)
	// - Multiple droplets per app per day (5-10 deployments/app/day) = ~30,000 droplets
	// - ~500 spaces + ~50 orgs (stable)
	// Total: ~35,000 entries over 12 hours
	bigcacheMaxEntriesInWindow = 100000
	// Max size of a single cache entry in bytes
	// CachedApp: ~300-500 bytes (with 3-4 labels/annotations)
	// CachedDroplet: ~200-400 bytes, CachedSpace/Org: ~200-400 bytes
	// 1KB is sufficient for entries with small metadata
	bigcacheMaxEntrySize = 1024
	// Disable stats collection for better performance
	bigcacheStatsEnabled = false
	// Interval between removing expired entries (clean up)
	bigcacheCleanWindow = 2 * time.Minute
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

type bigcacheLogger struct {
	l *zap.SugaredLogger
}

// CachedApp holds all app-related data to be stored in cache
type CachedApp struct {
	GUID        string
	Name        string
	State       string
	CreatedAt   string
	UpdatedAt   string
	Labels      map[string]*string
	Annotations map[string]*string
	SpaceGUID   string
	DropletGUID string

	// Lifecycle type from app (droplet has the full lifecycle data)
	LifecycleType string
}

// CachedDroplet holds droplet lifecycle data to be stored in cache
type CachedDroplet struct {
	GUID string

	// Lifecycle information
	LifecycleType string
	Buildpacks    []string
	Stack         string
	DockerImage   string
}

// CachedSpace holds all space-related data to be stored in cache
type CachedSpace struct {
	GUID        string
	Name        string
	Labels      map[string]*string
	Annotations map[string]*string
	OrgGUID     string
}

// CachedOrg holds all organization-related data to be stored in cache
type CachedOrg struct {
	GUID        string
	Name        string
	Labels      map[string]*string
	Annotations map[string]*string
}

func init() {
	// Register custom cache structures
	gob.Register(&CachedApp{})
	gob.Register(&CachedDroplet{})
	gob.Register(&CachedSpace{})
	gob.Register(&CachedOrg{})
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

func (log *bigcacheLogger) Printf(format string, v ...interface{}) {
	log.l.Infof(format, v...)
}

func (cfCli *Client) newCache() (*bigcache.BigCache, error) {
	logger := &bigcacheLogger{
		l: cfCli.logger.Sugar(),
	}
	config := bigcache.Config{
		Shards:             bigcacheShards,
		LifeWindow:         cfCli.cacheTTL,
		CleanWindow:        bigcacheCleanWindow,
		MaxEntriesInWindow: bigcacheMaxEntriesInWindow,
		MaxEntrySize:       bigcacheMaxEntrySize,
		StatsEnabled:       bigcacheStatsEnabled,
		HardMaxCacheSize:   0,
		Verbose:            bigcacheVerbose,
		Logger:             logger,
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

func (cfCli *Client) getApp(appID string) (*CachedApp, error) {
	var cachedApp CachedApp
	objCacheName := "app:" + appID
	if value, err := cfCli.cache.Get(objCacheName); err != nil {
		// Not found in cache
		cfCli.logger.Debug("Cache miss for app", zap.String("appID", appID))
		app, err := cfCli.cf.Applications.Get(cfCli.ctx, appID)
		if err != nil {
			err = fmt.Errorf("could not retrieve App with guid %s from Cloud Foundry API: %w", appID, err)
			return nil, err
		}
		cachedApp.GUID = app.GUID
		cachedApp.Name = app.Name
		cachedApp.State = app.State
		cachedApp.CreatedAt = app.CreatedAt.String()
		cachedApp.UpdatedAt = app.UpdatedAt.String()
		cachedApp.Labels = app.Metadata.Labels
		cachedApp.Annotations = app.Metadata.Annotations
		cachedApp.SpaceGUID = app.Relationships.Space.Data.GUID
		if app.Relationships.CurrentDroplet.Data != nil {
			cachedApp.DropletGUID = app.Relationships.CurrentDroplet.Data.GUID
		}
		cachedApp.LifecycleType = app.Lifecycle.Type
		data, err := serialize(&cachedApp)
		if err != nil {
			err = fmt.Errorf("could not encode CachedApp %s object to store in cache: %w", appID, err)
			return nil, err
		}
		cfCli.cache.Set(objCacheName, data)
	} else {
		cfCli.logger.Debug("Cache hit for app", zap.String("appID", appID))
		if err := deserialize(value, &cachedApp); err != nil {
			err = fmt.Errorf("could not decode CachedApp %s object stored in cache: %w", appID, err)
			return nil, err
		}
	}
	return &cachedApp, nil
}

func (cfCli *Client) GetAppMetadata(appID string) (map[string]*string, map[string]*string, error) {
	cachedApp, err := cfCli.getApp(appID)
	if err != nil {
		return nil, nil, err
	}
	return cachedApp.Labels, cachedApp.Annotations, nil
}

func (cfCli *Client) GetAppName(appID string) (string, error) {
	cachedApp, err := cfCli.getApp(appID)
	if err != nil {
		return "", err
	}
	return cachedApp.Name, nil
}

func (cfCli *Client) GetAppSpace(appID string) (string, error) {
	cachedApp, err := cfCli.getApp(appID)
	if err != nil {
		return "", err
	}
	return cachedApp.SpaceGUID, nil
}

func (cfCli *Client) GetAppState(appID string) (string, error) {
	cachedApp, err := cfCli.getApp(appID)
	if err != nil {
		return "", err
	}
	return cachedApp.State, nil
}

func (cfCli *Client) GetAppDates(appID string) (string, string, error) {
	cachedApp, err := cfCli.getApp(appID)
	if err != nil {
		return "", "", err
	}
	return cachedApp.CreatedAt, cachedApp.UpdatedAt, nil
}

func (cfCli *Client) getDroplet(dropletID string) (*CachedDroplet, error) {
	var cachedDroplet CachedDroplet
	objCacheName := "droplet:" + dropletID
	if value, err := cfCli.cache.Get(objCacheName); err != nil {
		droplet, err := cfCli.cf.Droplets.Get(cfCli.ctx, dropletID)
		if err != nil {
			err = fmt.Errorf("could not retrieve Droplet with guid %s from Cloud Foundry API: %w", dropletID, err)
			return nil, err
		}
		cachedDroplet.GUID = droplet.GUID
		cachedDroplet.LifecycleType = droplet.Lifecycle.Type
		switch droplet.Lifecycle.Type {
		case "buildpack":
			cachedDroplet.Stack = droplet.Stack
			if len(droplet.Buildpacks) > 0 {
				cachedDroplet.Buildpacks = make([]string, len(droplet.Buildpacks))
				for i, bp := range droplet.Buildpacks {
					cachedDroplet.Buildpacks[i] = bp.Name
				}
			}
		case "cnb":
			cachedDroplet.Stack = droplet.Stack
			if len(droplet.Buildpacks) > 0 {
				cachedDroplet.Buildpacks = make([]string, len(droplet.Buildpacks))
				for i, bp := range droplet.Buildpacks {
					cachedDroplet.Buildpacks[i] = bp.Name
				}
			}
		case "docker":
			cachedDroplet.DockerImage = *droplet.Image
		}
		data, err := serialize(&cachedDroplet)
		if err != nil {
			err = fmt.Errorf("could not encode CachedDroplet %s object to store in cache: %w", dropletID, err)
			return nil, err
		}
		cfCli.cache.Set(objCacheName, data)
	} else {
		if err := deserialize(value, &cachedDroplet); err != nil {
			err = fmt.Errorf("could not decode CachedDroplet %s object stored in cache: %w", dropletID, err)
			return nil, err
		}
	}
	return &cachedDroplet, nil
}

func (cfCli *Client) GetAppLifecycle(appID string) (string, []string, string, error) {
	cachedApp, err := cfCli.getApp(appID)
	if err != nil {
		return "", []string{}, "", err
	}
	if cachedApp.DropletGUID != "" {
		cachedDroplet, err := cfCli.getDroplet(cachedApp.DropletGUID)
		if err != nil {
			return cachedApp.LifecycleType, []string{}, "", err
		}
		switch cachedDroplet.LifecycleType {
		case "docker":
			return cachedDroplet.LifecycleType, []string{}, cachedDroplet.DockerImage, nil
		default: // buildpack, cnb
			return cachedDroplet.LifecycleType, cachedDroplet.Buildpacks, cachedDroplet.Stack, nil
		}
	}
	return cachedApp.LifecycleType, []string{}, "", nil
}

func (cfCli *Client) getSpace(spaceID string) (*CachedSpace, error) {
	var cachedSpace CachedSpace
	objCacheName := "space:" + spaceID
	if value, err := cfCli.cache.Get(objCacheName); err != nil {
		// Not found in cache
		space, err := cfCli.cf.Spaces.Get(cfCli.ctx, spaceID)
		if err != nil {
			err = fmt.Errorf("could not retrieve Space with guid %s from Cloud Foundry API: %w", spaceID, err)
			return nil, err
		}
		cachedSpace.GUID = space.GUID
		cachedSpace.Name = space.Name
		cachedSpace.Labels = space.Metadata.Labels
		cachedSpace.Annotations = space.Metadata.Annotations
		cachedSpace.OrgGUID = space.Relationships.Organization.Data.GUID
		data, err := serialize(&cachedSpace)
		if err != nil {
			err = fmt.Errorf("could not encode CachedSpace %s object to store in cache: %w", spaceID, err)
			return nil, err
		}
		cfCli.cache.Set(objCacheName, data)
	} else {
		if err := deserialize(value, &cachedSpace); err != nil {
			err = fmt.Errorf("could not decode CachedSpace %s object stored in cache: %w", spaceID, err)
			return nil, err
		}
	}
	return &cachedSpace, nil
}

func (cfCli *Client) GetSpaceName(spaceID string) (string, error) {
	cachedSpace, err := cfCli.getSpace(spaceID)
	if err != nil {
		return "", err
	}
	return cachedSpace.Name, nil
}

func (cfCli *Client) GetSpaceMetadata(spaceID string) (map[string]*string, map[string]*string, error) {
	cachedSpace, err := cfCli.getSpace(spaceID)
	if err != nil {
		return nil, nil, err
	}
	return cachedSpace.Labels, cachedSpace.Annotations, nil
}

func (cfCli *Client) GetSpaceOrg(spaceID string) (string, error) {
	cachedSpace, err := cfCli.getSpace(spaceID)
	if err != nil {
		return "", err
	}
	return cachedSpace.OrgGUID, nil
}

func (cfCli *Client) getOrg(orgID string) (*CachedOrg, error) {
	var cachedOrg CachedOrg
	objCacheName := "org:" + orgID
	if value, err := cfCli.cache.Get(objCacheName); err != nil {
		// Not found in cache
		org, err := cfCli.cf.Organizations.Get(cfCli.ctx, orgID)
		if err != nil {
			err = fmt.Errorf("could not retrieve Org with guid %s from Cloud Foundry API: %w", orgID, err)
			return nil, err
		}
		cachedOrg.GUID = org.GUID
		cachedOrg.Name = org.Name
		cachedOrg.Labels = org.Metadata.Labels
		cachedOrg.Annotations = org.Metadata.Annotations
		data, err := serialize(&cachedOrg)
		if err != nil {
			err = fmt.Errorf("could not encode CachedOrg %s object to store in cache: %w", orgID, err)
			return nil, err
		}
		cfCli.cache.Set(objCacheName, data)
	} else {
		if err := deserialize(value, &cachedOrg); err != nil {
			err = fmt.Errorf("could not decode CachedOrg %s object stored in cache: %w", orgID, err)
			return nil, err
		}
	}
	return &cachedOrg, nil
}

func (cfCli *Client) GetOrgName(orgID string) (string, error) {
	cachedOrg, err := cfCli.getOrg(orgID)
	if err != nil {
		return "", err
	}
	return cachedOrg.Name, nil
}

func (cfCli *Client) GetOrgMetadata(orgID string) (map[string]*string, map[string]*string, error) {
	cachedOrg, err := cfCli.getOrg(orgID)
	if err != nil {
		return nil, nil, err
	}
	return cachedOrg.Labels, cachedOrg.Annotations, nil
}
