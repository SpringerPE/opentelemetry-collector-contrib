package cfattributesprocessor // import "github.com/open-telemetry/opentelemetry-collector-contrib/processor/cfattributesprocessor"

import (
	"errors"
	"fmt"
	"time"
)

const (
	// authTypeClientCredentials uses a client ID and client secret to authenticate
	authTypeClientCredentials authType = "client_credentials"
	// authTypeUserPass uses username and password to authenticate
	authTypeUserPass authType = "user_pass"
	// authTypeToken uses access token and refresh token to authenticate
	authTypeToken authType = "token"
)

type authType string

type Config struct {
	// CloudFoundry API Configuration
	CloudFoundry CfConfig `mapstructure:"cloud_foundry"`

	// Defines the resource attribute key where the CF App guid is defined
	// Default: "app_id"
	AppIDAttributeKeyAssociation string `mapstructure:"appid_attribute_association"`

	// CacheTTL determines the time that CF objects (app, space, org) are kept in cache
	// to avoid querying the CF API. This setting impacts the metadata being changed by the
	// user.
	// Default: "5m"
	CacheTTL time.Duration `mapstructure:"cache_ttl"`

	// Attributes to include
	Extract CfTagExtract `mapstructure:"extract"`
}

type CfTagExtract struct {
	// Metadata
	Metadata CfTagExtractMetadata `mapstructure:"metadata"`

	// Determines whether or not App lifecycle information (buildpack and stack) gets add to resource attributes
	// Default: false
	AppStateLifecycle bool `mapstructure:"app_state_lifecycle"`

	// Determines whether or not App dates information (star and updated) gets add to resource attributes
	// Default: false
	AppDates bool `mapstructure:"app_dates"`
}

type CfTagExtractMetadata struct {
	// Determines whether or not Space labels and annotations get added to the resource attributes
	// Default: false
	Space bool `mapstructure:"space"`

	// Determines whether or not Space labels and annotations get added to the resource attributes
	// Default: false
	Org bool `mapstructure:"org"`

	// Determines whether or not Space labels and annotations get added to the resource attributes
	// Default: true
	App bool `mapstructure:"app"`
}

type CfConfig struct {
	// The URL of the CloudFoundry API
	Endpoint string `mapstructure:"endpoint"`

	// Authentication details
	Auth CfAuth `mapstructure:"auth"`
}

type CfAuth struct {
	// Authentication method, there are 3 options
	Type authType `mapstructure:"type"`

	// Used for user_pass authentication method
	Username string `mapstructure:"username"`
	Password string `mapstructure:"password"`

	// Used for token authentication method
	AccessToken  string `mapstructure:"access_token"`
	RefreshToken string `mapstructure:"refresh_token"`

	// Used for client_credentials authentication method
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
}

// Validate overrides the embedded noop validation so that load config can trigger
// our own validation logic.
func (config *Config) Validate() error {
	c := config.CloudFoundry
	if c.Endpoint == "" {
		return errors.New("CloudFoundry.Endpoint must be specified")
	}
	if c.Auth.Type == "" {
		return errors.New("CloudFoundry.Auth.Type must be specified")
	}

	switch c.Auth.Type {
	case authTypeUserPass:
		if c.Auth.Username == "" {
			return fieldError(authTypeUserPass, "username")
		}
		if c.Auth.Password == "" {
			return fieldError(authTypeUserPass, "password")
		}
	case authTypeClientCredentials:
		if c.Auth.ClientID == "" {
			return fieldError(authTypeClientCredentials, "client_id")
		}
		if c.Auth.ClientSecret == "" {
			return fieldError(authTypeClientCredentials, "client_secret")
		}
	case authTypeToken:
		if c.Auth.AccessToken == "" {
			return fieldError(authTypeToken, "access_token")
		}
		if c.Auth.RefreshToken == "" {
			return fieldError(authTypeToken, "refresh_token")
		}
	default:
		return fmt.Errorf("configuration option `auth_type` must be set to one of the following values: [user_pass, client_credentials, token]. Specified value: %s", c.Auth.Type)
	}
	return nil
}

func fieldError(authType authType, param string) error {
	return fmt.Errorf("%s is required when using auth_type: %s", param, authType)
}
