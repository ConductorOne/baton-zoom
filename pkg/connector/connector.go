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

var (
	resourceTypeUser = &v2.ResourceType{
		Id:          "user",
		DisplayName: "User",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_USER,
		},
		Annotations: annotationsForUserResourceType(),
	}
	resourceTypeGroup = &v2.ResourceType{
		Id:          "group",
		DisplayName: "Group",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_GROUP,
		},
	}
	resourceTypeContactGroup = &v2.ResourceType{
		Id:          "contactGroup",
		DisplayName: "Contact Group",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_GROUP,
		},
	}
	resourceTypeRole = &v2.ResourceType{
		Id:          "role",
		DisplayName: "Role",
		Traits: []v2.ResourceType_Trait{
			v2.ResourceType_TRAIT_ROLE,
		},
	}
)

type Zoom struct {
	client *zoom.Client
}

func New(
	ctx context.Context,
	accountId string,
	clientId string,
	clientSecret string,
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
		client: zoom.NewClient(httpClient, token),
	}, nil
}

func (z *Zoom) Metadata(_ context.Context) (*v2.ConnectorMetadata, error) {
	return &v2.ConnectorMetadata{
		DisplayName: "Zoom",
		Description: "Connector syncing users, groups, roles and contact groups from Zoom to Baton.",
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
				"first_name": {
					DisplayName: "First Name",
					Required:    true,
					Description: "First name of the person who will own the user.",
					Field: &v2.ConnectorAccountCreationSchema_Field_StringField{
						StringField: &v2.ConnectorAccountCreationSchema_StringField{},
					},
					Placeholder: "John",
					Order:       2,
				},
				"last_name": {
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

func (z *Zoom) ResourceSyncers(ctx context.Context) []connectorbuilder.ResourceSyncer {
	return []connectorbuilder.ResourceSyncer{
		userBuilder(z.client),
		groupBuilder(z.client),
		roleBuilder(z.client),
		contactGroupBuilder(z.client),
	}
}
