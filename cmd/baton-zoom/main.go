package main

import (
	"context"
	"fmt"
	"os"

	configSchema "github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/field"
	"github.com/conductorone/baton-sdk/pkg/types"
	"github.com/conductorone/baton-zoom/pkg/connector"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"github.com/spf13/viper"
	"go.uber.org/zap"
)

const (
	version           = "dev"
	connectorName     = "baton-zoom"
	batonCacheDisable = "cache-disable"
	batonCacheTTL     = "cache-ttl"
	batonCacheMaxSize = "cache-max-size"
)

var (
	accountId           = field.StringField(connector.AccountId, field.WithRequired(true), field.WithDescription("Account ID used to generate token providing access to Zoom API."))
	zoomClientId        = field.StringField(connector.ZoomClientId, field.WithRequired(true), field.WithDescription("Client ID used to generate token providing access to Zoom API."))
	zoomClientSecret    = field.StringField(connector.ZoomClientSecret, field.WithRequired(true), field.WithDescription("Client Secret used to generate token providing access to Zoom API."))
	CacheDisabled       = field.StringField(batonCacheDisable, field.WithRequired(false), field.WithDescription("Verbose mode shows information about new memory allocation."))
	CacheTTL            = field.StringField(batonCacheTTL, field.WithRequired(false), field.WithDescription("Time after which entry can be evicted."))
	CacheMaxSize        = field.StringField(batonCacheMaxSize, field.WithRequired(false), field.WithDescription("It is a limit for BytesQueue size in MB."))
	configurationFields = []field.SchemaField{accountId, zoomClientId, zoomClientSecret, CacheDisabled, CacheTTL, CacheMaxSize}
)

func main() {
	ctx := context.Background()
	_, cmd, err := configSchema.DefineConfiguration(ctx,
		connectorName,
		getConnector,
		field.NewConfiguration(configurationFields),
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}

	cmd.Version = version
	err = cmd.Execute()
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func getConnector(ctx context.Context, cfg *viper.Viper) (types.ConnectorServer, error) {
	l := ctxzap.Extract(ctx)
	cb, err := connector.New(ctx, cfg)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}

	c, err := connectorbuilder.NewConnector(ctx, cb)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, err
	}

	return c, nil
}
