package connector

import (
	"context"
	"fmt"

	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
)

type Zoom struct {
	client            *zoom.Client
	syncInactiveUsers bool
}

func NewForCapabilities() *Zoom {
	return &Zoom{syncInactiveUsers: true}
}

func New(
	ctx context.Context,
	accountId string,
	clientId string,
	clientSecret string,
	syncInactiveUsers bool,
	baseURL string,
) (*Zoom, error) {
	httpClient, err := uhttp.NewClient(ctx, uhttp.WithLogger(true, ctxzap.Extract(ctx)))
	if err != nil {
		return nil, err
	}

	token, err := zoom.RequestAccessToken(ctx, accountId, clientId, clientSecret)
	if err != nil {
		return nil, fmt.Errorf("zoom-connector: failed to get token: %w", err)
	}

	return &Zoom{
		client:            zoom.NewClient(httpClient, token, baseURL),
		syncInactiveUsers: syncInactiveUsers,
	}, nil
}

func (z *Zoom) Metadata(_ context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "Zoom",
		Description: "Connector syncing users, groups, roles, contact groups, and license tiers from Zoom to Baton.",
		AccountCreationSchema: &v2.ConnectorAccountCreationSchema{
			FieldMap: map[string]*v2.ConnectorAccountCreationSchema_Field{
				"email": {
					DisplayName: "Email",
					Required:    true,
					Description: "This email will be used as the login for the user.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "john.doe@example.com",
					Order:       1,
				},
				firstNameKey: {
					DisplayName: "First Name",
					Required:    true,
					Description: "First name of the person who will own the user.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "John",
					Order:       2,
				},
				lastNameKey: {
					DisplayName: "Last Name",
					Required:    true,
					Description: "Last name of the person who will own the user.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "Doe",
					Order:       3,
				},
				"display_name": {
					DisplayName: "Display Name",
					Required:    true,
					Description: "This is the name that will be displayed on the new account.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "John Doe",
					Order:       4,
				},
			},
		},
	}, nil
}

func (z *Zoom) Validate(ctx context.Context) (annotations.Annotations, error) {
	user, resp, err := z.client.GetUser(ctx, "me")
	if err != nil {
		return nil, fmt.Errorf("zoom-connector: failed to get current user: %w", err)
	}
	resp.Body.Close()

	// all required scopes are for admins only
	if user.RoleName == "member" {
		return nil, fmt.Errorf("zoom-connector: user is not an admin")
	}

	return nil, nil
}

func (z *Zoom) Close() error {
	return nil
}

func (z *Zoom) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncerV2 {
	return []connectorbuilder.ResourceSyncerV2{
		userBuilder(z.client, z.syncInactiveUsers),
		inviteBuilder(z.client),
		groupBuilder(z.client),
		roleBuilder(z.client),
		contactGroupBuilder(z.client),
		licenseBuilder(z.client, z.syncInactiveUsers),
	}
}
