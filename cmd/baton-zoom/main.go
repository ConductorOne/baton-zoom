package main

import (
	"context"

	sdkconfig "github.com/conductorone/baton-sdk/pkg/config"
	"github.com/conductorone/baton-sdk/pkg/cli"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/connectorrunner"
	"github.com/conductorone/baton-zoom/pkg/config"
	"github.com/conductorone/baton-zoom/pkg/connector"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	version       = "dev"
	connectorName = "baton-zoom"
)

func main() {
	ctx := context.Background()
	sdkconfig.RunConnector(ctx,
		connectorName,
		version,
		config.Config,
		getConnector,
		connectorrunner.WithDefaultCapabilitiesConnectorBuilderV2(connector.NewForCapabilities()),
	)
}

func getConnector(ctx context.Context, cfg *config.Zoom, _ *cli.ConnectorOpts) (connectorbuilder.ConnectorBuilderV2, []connectorbuilder.Opt, error) {
	l := ctxzap.Extract(ctx)

	cb, err := connector.New(
		ctx,
		cfg.GetString(config.AccountIdField.FieldName),
		cfg.GetString(config.ZoomClientIdField.FieldName),
		cfg.GetString(config.ZoomClientSecretField.FieldName),
	)
	if err != nil {
		l.Error("error creating connector", zap.Error(err))
		return nil, nil, err
	}

	return cb, nil, nil
}
