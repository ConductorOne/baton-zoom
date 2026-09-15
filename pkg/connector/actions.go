package connector

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	config "github.com/conductorone/baton-sdk/pb/c1/config/v1"
	v2 "github.com/conductorone/baton-sdk/pb/c1/connector/v2"
	"github.com/conductorone/baton-sdk/pkg/actions"
	"github.com/conductorone/baton-sdk/pkg/annotations"
	"github.com/conductorone/baton-sdk/pkg/connectorbuilder"
	"github.com/conductorone/baton-zoom/pkg/zoom"
	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/structpb"
)

const (
	actionTransferAndDeleteUser = "transfer_and_delete_user"

	argUserID            = "user_id"
	argDeleteAction      = "action"
	argTransferEmail     = "transfer_email"
	argTransferMeeting   = "transfer_meeting"
	argTransferWebinar   = "transfer_webinar"
	argTransferRecording = "transfer_recording"
)

var transferAndDeleteUserSchema = &v2.BatonActionSchema{
	Name:        actionTransferAndDeleteUser,
	DisplayName: "Transfer Data and Delete User",
	Description: "Reassigns a user's meetings, webinars, and cloud recordings to another Zoom user, then removes the user from the account. This operation cannot be recovered.",
	Arguments: []*config.Field{
		{
			Name:        argUserID,
			DisplayName: "User",
			Description: "The Zoom user to remove.",
			IsRequired:  true,
			Field: &config.Field_ResourceIdField{
				ResourceIdField: &config.ResourceIdField{
					Rules: &config.ResourceIDRules{
						AllowedResourceTypeIds: []string{resourceTypeUser.Id},
					},
				},
			},
		},
		{
			Name:        argDeleteAction,
			DisplayName: "Action",
			Description: "Whether to disassociate the user from the account or permanently delete them.",
			IsRequired:  true,
			Field: &config.Field_StringField{
				StringField: &config.StringField{
					Rules: &config.StringRules{
						In: []string{string(zoom.Disassociate), string(zoom.Delete)},
					},
					Options: []*config.StringFieldOption{
						{Name: string(zoom.Disassociate), Value: string(zoom.Disassociate), DisplayName: "Disassociate"},
						{Name: string(zoom.Delete), Value: string(zoom.Delete), DisplayName: "Delete"},
					},
				},
			},
		},
		{
			Name:        argTransferEmail,
			DisplayName: "Transfer To",
			Description: "Email of the Zoom user to receive the transferred meetings, webinars, and recordings. Required if any transfer option below is enabled.",
			Field:       &config.Field_StringField{},
		},
		{
			Name:        argTransferMeeting,
			DisplayName: "Transfer Meetings",
			Description: "Transfer the user's scheduled meetings to the Transfer To user.",
			Field:       &config.Field_BoolField{},
		},
		{
			Name:        argTransferWebinar,
			DisplayName: "Transfer Webinars",
			Description: "Transfer the user's scheduled webinars to the Transfer To user.",
			Field:       &config.Field_BoolField{},
		},
		{
			Name:        argTransferRecording,
			DisplayName: "Transfer Cloud Recordings",
			Description: "Transfer the user's cloud recordings to the Transfer To user.",
			Field:       &config.Field_BoolField{},
		},
	},
	ReturnTypes: []*config.Field{
		{Name: "success", DisplayName: "Success", Field: &config.Field_BoolField{}},
		{Name: "message", DisplayName: "Message", Field: &config.Field_StringField{}},
	},
	ActionType: []v2.ActionType{v2.ActionType_ACTION_TYPE_RESOURCE_DELETE},
}

var _ connectorbuilder.ResourceActionProvider = (*userResourceType)(nil)

func (u *userResourceType) ResourceActions(ctx context.Context, registry actions.ActionRegistry) error {
	schema := proto.Clone(transferAndDeleteUserSchema).(*v2.BatonActionSchema)
	if err := registry.Register(ctx, schema, u.transferAndDeleteUserAction); err != nil {
		return fmt.Errorf("baton-zoom: register transfer_and_delete_user action: %w", err)
	}
	return nil
}

func (u *userResourceType) transferAndDeleteUserAction(
	ctx context.Context,
	args *structpb.Struct,
) (*structpb.Struct, annotations.Annotations, error) {
	userRef, err := actions.RequireResourceIDArg(args, argUserID)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: %v", err)
	}
	userID := userRef.GetResource()
	if userID == "" {
		return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: user_id is required")
	}
	// Local --invoke-action calls bypass the schema's resource type check.
	if resourceType := userRef.GetResourceType(); resourceType != resourceTypeUser.Id {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: user_id must reference a %q resource, got %q", resourceTypeUser.Id, resourceType)
	}
	// Reject values url.JoinPath could normalize to another endpoint.
	if invalidUserPathSegment(userID) {
		return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: user_id must not contain path separators or standalone dot segments")
	}

	deleteAction, err := actions.RequireStringArg(args, argDeleteAction)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: %v", err)
	}
	if deleteAction != string(zoom.Disassociate) && deleteAction != string(zoom.Delete) {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: action must be %q or %q", zoom.Disassociate, zoom.Delete)
	}

	transferEmail, err := optionalStringArg(args, argTransferEmail)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: %v", err)
	}
	transferMeeting, err := optionalBoolArg(args, argTransferMeeting)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: %v", err)
	}
	transferWebinar, err := optionalBoolArg(args, argTransferWebinar)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: %v", err)
	}
	transferRecording, err := optionalBoolArg(args, argTransferRecording)
	if err != nil {
		return nil, nil, status.Errorf(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: %v", err)
	}
	transferring := transferMeeting || transferWebinar || transferRecording

	if transferring && transferEmail == "" {
		return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: transfer_email is required when transfer_meeting, transfer_webinar, or transfer_recording is set")
	}
	if transferEmail != "" && !transferring {
		return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: at least one transfer option is required when transfer_email is set")
	}
	if invalidUserPathSegment(transferEmail) {
		return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: transfer_email must not contain path separators or standalone dot segments")
	}

	// Confirm the recipient first because Zoom's DELETE response does not
	// identify which user caused a 404.
	rateLimitAnnos := annotations.Annotations{}
	if transferEmail != "" {
		_, annos, err := u.client.GetUser(ctx, transferEmail)
		if err != nil {
			if zoom.IsAPIError(err, http.StatusNotFound, zoom.UserNotFoundErrorCode) {
				return nil, nil, status.Error(codes.InvalidArgument, "baton-zoom: transfer_and_delete_user: transfer recipient was not found")
			}
			return nil, nil, fmt.Errorf("baton-zoom: transfer_and_delete_user: verify transfer recipient: %w", err)
		}
		rateLimitAnnos = annos
	}

	// Do not log transfer_email because it is PII. Keep it out of returned
	// errors too: the SDK logs handler errors.
	ctxzap.Extract(ctx).Info("baton-zoom: transfer_and_delete_user: removing user",
		zap.String("user_id", userID),
		zap.String("action", deleteAction),
		zap.Bool("transfer_meeting", transferMeeting),
		zap.Bool("transfer_webinar", transferWebinar),
		zap.Bool("transfer_recording", transferRecording),
	)

	err = u.client.DeleteUser(ctx, userID, zoom.DeleteUserOptions{
		Action:            zoom.DeleteAction(deleteAction),
		TransferEmail:     transferEmail,
		TransferMeeting:   transferMeeting,
		TransferWebinar:   transferWebinar,
		TransferRecording: transferRecording,
	})
	if err != nil {
		if zoom.IsAPIError(err, http.StatusNotFound, zoom.UserNotFoundErrorCode) {
			if !transferring {
				return actions.NewReturnValues(
					true,
					actions.NewStringReturnField("message", fmt.Sprintf("user %s was already removed from the account", userID)),
				), rateLimitAnnos, nil
			}
			// Recipient validation does not prove the transfer completed, and
			// Zoom cannot distinguish prior success from a failed transfer.
			return nil, rateLimitAnnos, status.Errorf(codes.FailedPrecondition,
				"baton-zoom: transfer_and_delete_user: user %s was already removed from the account, but the requested transfer cannot be confirmed; verify manually", userID)
		}
		return nil, rateLimitAnnos, fmt.Errorf("baton-zoom: transfer_and_delete_user: %s: %w", userID, err)
	}

	message := fmt.Sprintf("user %s %sd from the account", userID, deleteAction)
	if transferring {
		message = fmt.Sprintf("user %s data transferred and %sd from the account", userID, deleteAction)
	}
	return actions.NewReturnValues(true, actions.NewStringReturnField("message", message)), rateLimitAnnos, nil
}

func optionalStringArg(args *structpb.Struct, key string) (string, error) {
	value, ok := args.GetFields()[key]
	if !ok || value == nil {
		return "", nil
	}
	if _, ok := value.GetKind().(*structpb.Value_NullValue); ok {
		return "", nil
	}
	if stringValue, ok := actions.GetStringArg(args, key); ok {
		return stringValue, nil
	}
	return "", fmt.Errorf("%s must be a string", key)
}

func invalidUserPathSegment(value string) bool {
	return value == "." || value == ".." || strings.Contains(value, "/")
}

func optionalBoolArg(args *structpb.Struct, key string) (bool, error) {
	value, ok := args.GetFields()[key]
	if !ok || value == nil {
		return false, nil
	}
	if _, ok := value.GetKind().(*structpb.Value_NullValue); ok {
		return false, nil
	}
	if boolValue, ok := actions.GetBoolArg(args, key); ok {
		return boolValue, nil
	}
	return false, fmt.Errorf("%s must be a boolean", key)
}
