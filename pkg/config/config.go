package config

import "github.com/conductorone/baton-sdk/pkg/field"

var (
	AccountIdField = field.StringField(
		"account-id",
		field.WithRequired(true),
		field.WithDescription("Account ID used to generate token providing access to Zoom API."),
		field.WithDisplayName("Account ID"),
	)
	ZoomClientIdField = field.StringField(
		"zoom-client-id",
		field.WithRequired(true),
		field.WithDescription("Client ID used to generate token providing access to Zoom API."),
		field.WithDisplayName("Client ID"),
	)
	ZoomClientSecretField = field.StringField(
		"zoom-client-secret",
		field.WithRequired(true),
		field.WithDescription("Client Secret used to generate token providing access to Zoom API."),
		field.WithDisplayName("Client Secret"),
		field.WithIsSecret(true),
	)
	SyncInactiveUsersField = field.BoolField(
		"sync-inactive-users",
		field.WithDescription("Sync inactive Zoom users alongside active users."),
		field.WithDisplayName("Sync Inactive Users"),
	)
	ConfigurationFields = []field.SchemaField{
		AccountIdField,
		ZoomClientIdField,
		ZoomClientSecretField,
		SyncInactiveUsersField,
	}
)

//go:generate go run ./gen
var Config = field.NewConfiguration(
	ConfigurationFields,
	field.WithConnectorDisplayName("Zoom"),
	field.WithHelpUrl("/docs/baton/zoom"),
	field.WithIconUrl("/static/app-icons/zoom.svg"),
)
