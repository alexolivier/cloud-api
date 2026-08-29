// Copyright 2021-2026 Zenauth Ltd.
// SPDX-License-Identifier: Apache-2.0

package notifications_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"buf.build/go/protovalidate"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"

	notificationsv1 "github.com/cerbos/cloud-api/genpb/cerbos/cloud/notifications/v1"
)

const (
	workspaceID  = "B6C0NNZO5VO6"
	deploymentID = "D4F8H2J6L0N3"
	buildID      = "X9K2M4P7Q1R3S5T8"
	channelID    = "C7H1K5M9P3R6"
	clientID     = "K2L4N6P8R0T2"
	memberID     = "M1N3P5R7T9V1"
)

var memberActor = &notificationsv1.Actor{
	Actor: &notificationsv1.Actor_MemberId{MemberId: memberID},
}

// payloads builds a minimal valid payload for every event type. It is the
// fixture set for the whole-contract tests below, so a new EventType without an
// entry here fails TestEveryEventTypeValidates.
var payloads = map[notificationsv1.EventType]func(*notificationsv1.Event){
	notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_BuildFailed{
			BuildFailed: &notificationsv1.BuildFailed{
				Build: build(),
				Failure: &notificationsv1.BuildFailed_Compilation_{
					Compilation: &notificationsv1.BuildFailed_Compilation{
						Errors: []*notificationsv1.BuildFailed_Compilation_Error{{
							Kind:  notificationsv1.BuildFailed_Compilation_Error_KIND_COMPILE_ERROR,
							File:  "resource_policies/leave_request.yaml",
							Error: "undefined variable 'reqest'",
							Position: &notificationsv1.BuildFailed_Compilation_Error_Position{
								Line:   12,
								Column: 9,
								Path:   "$.resourcePolicy.rules[0].condition",
							},
						}},
						ErrorCount: 1,
					},
				},
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_BUILD_SUCCEEDED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_BuildSucceeded{
			BuildSucceeded: &notificationsv1.BuildSucceeded{
				Build: build(),
				Tests: &notificationsv1.TestTally{TestsCount: 3, PassedCount: 3},
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_BUNDLE_PROMOTED: func(e *notificationsv1.Event) {
		e.DeploymentId = new(deploymentID)
		e.Payload = &notificationsv1.Event_BundlePromoted{
			BundlePromoted: &notificationsv1.BundlePromoted{BuildId: buildID},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_CREDENTIAL_CREATED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_CredentialCreated{
			CredentialCreated: &notificationsv1.CredentialCreated{
				ClientId:    clientID,
				Description: "CI deploy",
				Actor:       memberActor,
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_CREDENTIAL_DELETED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_CredentialDeleted{
			CredentialDeleted: &notificationsv1.CredentialDeleted{ClientId: clientID, Actor: memberActor},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_MEMBER_ADDED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_MemberAdded{
			MemberAdded: &notificationsv1.MemberAdded{
				Member: &notificationsv1.Member{Id: memberID, Email: new("alice@example.com")},
				Roles:  []notificationsv1.WorkspaceRole{notificationsv1.WorkspaceRole_WORKSPACE_ROLE_DEVELOPER},
				Actor:  memberActor,
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_MEMBER_REMOVED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_MemberRemoved{
			MemberRemoved: &notificationsv1.MemberRemoved{
				Member: &notificationsv1.Member{Id: memberID},
				Roles:  []notificationsv1.WorkspaceRole{notificationsv1.WorkspaceRole_WORKSPACE_ROLE_VIEWER},
				Actor:  memberActor,
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_CHANNEL_DISABLED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_ChannelDisabled{
			ChannelDisabled: &notificationsv1.ChannelDisabled{
				ChannelId: channelID,
				Reason:    notificationsv1.ChannelDisabled_REASON_DELIVERY_FAILURES,
				Details:   "503 Service Unavailable",
			},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_TEST_FIRED: func(e *notificationsv1.Event) {
		e.Payload = &notificationsv1.Event_TestFired{
			TestFired: &notificationsv1.TestFired{ChannelId: channelID, Actor: memberActor},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_DEPLOYMENT_FLEET_DARK: func(e *notificationsv1.Event) {
		e.DeploymentId = new(deploymentID)
		e.Payload = &notificationsv1.Event_DeploymentFleetDark{
			DeploymentFleetDark: &notificationsv1.DeploymentFleetDark{Condition: condition()},
		}
	},
	notificationsv1.EventType_EVENT_TYPE_DEPLOYMENT_BUNDLE_LAGGING: func(e *notificationsv1.Event) {
		e.DeploymentId = new(deploymentID)
		e.Payload = &notificationsv1.Event_DeploymentBundleLagging{
			DeploymentBundleLagging: &notificationsv1.DeploymentBundleLagging{Condition: condition()},
		}
	},
}

func build() *notificationsv1.Build {
	return &notificationsv1.Build{
		Id:            buildID,
		StoreIds:      []string{"S1T2U3V4W5X6"},
		DeploymentIds: []string{deploymentID},
	}
}

func condition() *notificationsv1.Condition {
	return &notificationsv1.Condition{
		State: notificationsv1.ConditionState_CONDITION_STATE_ENTERED,
		Since: timestamppb.Now(),
	}
}

func event(t *testing.T, eventType notificationsv1.EventType) *notificationsv1.Event {
	t.Helper()

	payload, ok := payloads[eventType]
	require.True(t, ok, "no fixture for %s", eventType)

	e := &notificationsv1.Event{
		Id:          "E1E2E3E4E5E6",
		Type:        eventType,
		OccurredAt:  timestamppb.Now(),
		WorkspaceId: workspaceID,
		Link:        "https://hub.cerbos.cloud/w/" + workspaceID,
	}
	payload(e)
	return e
}

// The webhook body documented in notifications.proto is protojson of the Event:
// the type as its enum name and the payload under a key named after the type.
func TestEventWebhookJSON(t *testing.T) {
	e := event(t, notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED)
	require.NoError(t, protovalidate.Validate(e), "fixture must be a valid event")

	body, err := protojson.Marshal(e)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "EVENT_TYPE_BUILD_FAILED", decoded["type"])
	require.Contains(t, decoded, "buildFailed")

	var roundTripped notificationsv1.Event
	require.NoError(t, protojson.Unmarshal(body, &roundTripped))
	require.Empty(t, cmp.Diff(e, &roundTripped, protocmp.Transform()))
}

// Every declared event type has a minimal valid event, so a transposed number
// in the type/payload CEL rule or a wrong entry in the deployment-scoped list
// shows up here.
func TestEveryEventTypeValidates(t *testing.T) {
	for _, eventType := range declaredEventTypes() {
		t.Run(eventType.String(), func(t *testing.T) {
			require.NoError(t, protovalidate.Validate(event(t, eventType)))
		})
	}
}

// The type/payload rule is a hand-written list of `this.type == N` clauses;
// adding an EventType without extending it would let the new type pass with
// the wrong payload.
func TestTypeMatchesPayloadRuleCoversEveryType(t *testing.T) {
	opts := (&notificationsv1.Event{}).ProtoReflect().Descriptor().Options()
	rules, ok := proto.GetExtension(opts, validate.E_Message).(*validate.MessageRules)
	require.True(t, ok, "Event must carry buf.validate.message rules")

	var expression string
	for _, rule := range rules.GetCel() {
		if rule.GetId() == "event.type_matches_payload" {
			expression = rule.GetExpression()
		}
	}
	require.NotEmpty(t, expression, "rule event.type_matches_payload not found")

	for _, eventType := range declaredEventTypes() {
		require.True(t, strings.Contains(expression, fmt.Sprintf("this.type == %d)", eventType)),
			"%s (%d) is missing from event.type_matches_payload", eventType, eventType)
	}
}

func TestEventValidation(t *testing.T) {
	testCases := []struct {
		name      string
		eventType notificationsv1.EventType
		mutate    func(*notificationsv1.Event)
		wantRule  string
	}{
		{
			name:      "workspace id of the wrong shape",
			eventType: notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED,
			mutate:    func(e *notificationsv1.Event) { e.WorkspaceId = "b6c0nnzo5vo6" },
			wantRule:  "string.pattern",
		},
		{
			name:      "type names a different payload",
			eventType: notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED,
			mutate:    func(e *notificationsv1.Event) { e.Type = notificationsv1.EventType_EVENT_TYPE_BUILD_SUCCEEDED },
			wantRule:  "event.type_matches_payload",
		},
		{
			name:      "deployment-scoped event without a deployment",
			eventType: notificationsv1.EventType_EVENT_TYPE_DEPLOYMENT_FLEET_DARK,
			mutate:    func(e *notificationsv1.Event) { e.DeploymentId = nil },
			wantRule:  "event.deployment_scoped",
		},
		{
			name:      "test failure in which nothing failed",
			eventType: notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED,
			mutate: func(e *notificationsv1.Event) {
				e.GetBuildFailed().Failure = &notificationsv1.BuildFailed_Tests{
					Tests: &notificationsv1.TestTally{TestsCount: 3, PassedCount: 3},
				}
			},
			wantRule: "build_failed.tests.some_failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			e := event(t, tc.eventType)
			tc.mutate(e)

			err := protovalidate.Validate(e)
			require.Error(t, err)

			var validationErr *protovalidate.ValidationError
			require.ErrorAs(t, err, &validationErr)
			require.Contains(t, ruleIDs(validationErr), tc.wantRule, "violations: %v", validationErr)
		})
	}
}

// Builds without an affected deployment are still valid events.
func TestBuildEventWithoutDeployments(t *testing.T) {
	e := event(t, notificationsv1.EventType_EVENT_TYPE_BUILD_FAILED)
	e.GetBuildFailed().GetBuild().DeploymentIds = nil
	require.NoError(t, protovalidate.Validate(e))
}

func declaredEventTypes() []notificationsv1.EventType {
	types := make([]notificationsv1.EventType, 0, len(notificationsv1.EventType_name))
	for n := range notificationsv1.EventType_name {
		if n != 0 {
			types = append(types, notificationsv1.EventType(n))
		}
	}
	return types
}

func ruleIDs(err *protovalidate.ValidationError) []string {
	ids := make([]string, 0, len(err.Violations))
	for _, v := range err.Violations {
		ids = append(ids, v.Proto.GetRuleId())
	}
	return ids
}
