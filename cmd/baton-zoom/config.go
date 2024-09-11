package main

import (
	"github.com/conductorone/baton-sdk/pkg/field"
)

var (
	AccountIdField = field.StringField(
		"account-id",
		field.WithRequired(true),
		field.WithDescription("Account ID used to generate token providing access to Zoom API."),
	)
	ZoomClientIdField = field.StringField(
		"zoom-client-id",
		field.WithRequired(true),
		field.WithDescription("Client ID used to generate token providing access to Zoom API."),
	)
	ZoomClientSecretField = field.StringField(
		"zoom-client-secret",
		field.WithRequired(true),
		field.WithDescription("Client Secret used to generate token providing access to Zoom API."),
	)
	ConfigurationFields = []field.SchemaField{
		AccountIdField,
		ZoomClientIdField,
		ZoomClientSecretField,
	}
)
