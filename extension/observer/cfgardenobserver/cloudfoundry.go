package cfgardenobserver

import (
	"fmt"

	"github.com/cloudfoundry/go-cfclient/v3/client"
	"github.com/cloudfoundry/go-cfclient/v3/config"
)

func NewCfClient(cfConfig CfConfig) (*client.Client, error) {
	var cfg *config.Config
	var err error

	switch cfConfig.AuthType {
	case AuthTypeUserPass:
		cfg, err = config.New(cfConfig.Endpoint, config.UserPassword(cfConfig.Username, cfConfig.Password))
	case AuthTypeClientCredentials:
		cfg, err = config.New(cfConfig.Endpoint, config.ClientCredentials(cfConfig.ClientID, cfConfig.ClientSecret))
	case AuthTypeToken:
		cfg, err = config.New(cfConfig.Endpoint, config.Token(cfConfig.AccessToken, cfConfig.RefreshToken))
	}

	if err != nil {
		return nil, fmt.Errorf("error creating connection to CloudFoundry API: %v", err)
	}

	c, err := client.New(cfg)
	if err != nil {
		return nil, err
	}
	return c, nil
}
