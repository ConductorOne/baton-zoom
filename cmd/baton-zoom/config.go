package main

import (
	"context"
	"fmt"

	"github.com/conductorone/baton-sdk/pkg/cli"
	"github.com/spf13/cobra"
)

// config defines the external configuration required for the connector to run.
type config struct {
	cli.BaseConfig `mapstructure:",squash"` // Puts the base config options in the same place as the connector options

	AccountID    string `mapstructure:"account-id"`
	ClientID     string `mapstructure:"client-id"`
	ClientSecret string `mapstructure:"client-secret"`
}

// validateConfig is run after the configuration is loaded, and should return an error if it isn't valid.
func validateConfig(ctx context.Context, cfg *config) error {
	if cfg.AccountID == "" {
		return fmt.Errorf("account id is missing")
	}
	if cfg.ClientID == "" {
		return fmt.Errorf("client id is missing")
	}
	if cfg.ClientSecret == "" {
		return fmt.Errorf("client secret is missing")
	}

	return nil
}

// cmdFlags sets the cmdFlags required for the connector.
func cmdFlags(cmd *cobra.Command) {
	cmd.PersistentFlags().String("account-id", "", "Account ID used to generate token providing access to Zoom API. ($BATON_ACCOUNT_ID)")
	cmd.PersistentFlags().String("client-id", "", "Client ID used to generate token providing access to Zoom API. ($BATON_CLIENT_ID)")
	cmd.PersistentFlags().String("client-secret", "", "Client Secret used to generate token providing access to Zoom API. ($BATON_CLIENT_SECRET)")
}
