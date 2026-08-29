// Copyright 2021-2026 Zenauth Ltd.
// SPDX-License-Identifier: Apache-2.0

package accessrequest_test

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"buf.build/go/protovalidate"
	enginev1 "github.com/cerbos/cerbos/api/genpb/cerbos/engine/v1"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"

	accessrequestv1 "github.com/cerbos/cloud-api/genpb/cerbos/cloud/accessrequest/v1"
)

const (
	requestID    = "R1S2T3U4V5W6"
	approvalID   = "A1B2C3D4E5F6"
	evaluationID = "01J9Z7QK3M5N8P2R4T6V8X0Y1A"
	// Stands in for an approval JWT; only its presence matters to the rules.
	placeholderJWT = "header.payload.signature"
)

func marker() *accessrequestv1.AccessRequestMarker {
	return &accessrequestv1.AccessRequestMarker{
		Approver:    "dba-oncall",
		ApprovalTtl: durationpb.New(4 * time.Hour),
		Reason:      "writes to production need a DBA",
	}
}

func denial() *accessrequestv1.DeniedEvaluation {
	return &accessrequestv1.DeniedEvaluation{
		EvaluationId: evaluationID,
		EvaluatedAt:  timestamppb.Now(),
		Principal:    &enginev1.Principal{Id: "alice", Roles: []string{"engineer"}},
		Resource:     &enginev1.Resource{Kind: "production_database", Id: "orders"},
		Actions:      []string{"write"},
		Marker:       marker(),
		RuleSrc:      "resource.production_database.vdefault#write-requestable",
	}
}

func submitRequest() *accessrequestv1.SubmitAccessRequestRequest {
	return &accessrequestv1.SubmitAccessRequestRequest{
		IdempotencyKey: new("INC-1234/orders/write"),
		Denial:         denial(),
		Justification:  "hotfix for INC-1234",
		Actor: &accessrequestv1.Actor{
			Id:   "deploy-bot",
			Type: "agent",
			Act:  []*accessrequestv1.Actor{{Id: "alice", Type: "user"}},
		},
	}
}

// actorChain builds a chain of the given depth: each actor acts for one more.
func actorChain(depth int) *accessrequestv1.Actor {
	a := &accessrequestv1.Actor{Id: "actor-1", Type: "user"}
	for i := 2; i <= depth; i++ {
		a = &accessrequestv1.Actor{Id: fmt.Sprintf("actor-%d", i), Type: "agent", Act: []*accessrequestv1.Actor{a}}
	}
	return a
}

func accessRequest(status accessrequestv1.Status) *accessrequestv1.AccessRequest {
	now := timestamppb.Now()
	ar := &accessrequestv1.AccessRequest{
		Id:            requestID,
		Status:        status,
		Denial:        denial(),
		Verification:  accessrequestv1.Verification_VERIFICATION_MATCHED,
		Justification: "hotfix for INC-1234",
		CreatedAt:     now,
		ExpiresAt:     timestamppb.New(now.AsTime().Add(time.Hour)),
	}
	if status == accessrequestv1.Status_STATUS_APPROVED {
		ar.Approval = &accessrequestv1.Approval{
			Id:            approvalID,
			ApprovedAt:    now,
			ApprovedUntil: timestamppb.New(now.AsTime().Add(4 * time.Hour)),
			Token:         placeholderJWT,
		}
	}
	return ar
}

func TestSubmitAccessRequestRequestValidation(t *testing.T) {
	require.NoError(t, protovalidate.Validate(submitRequest()), "fixture must be a valid request")

	t.Run("without optional fields", func(t *testing.T) {
		req := &accessrequestv1.SubmitAccessRequestRequest{Denial: denial()}
		require.NoError(t, protovalidate.Validate(req))
	})

	t.Run("actor chain three levels deep", func(t *testing.T) {
		req := submitRequest()
		req.Actor = actorChain(3)
		require.NoError(t, protovalidate.Validate(req))
	})

	t.Run("ttl at the bounds", func(t *testing.T) {
		for _, ttl := range []time.Duration{time.Minute, 720 * time.Hour} {
			req := submitRequest()
			req.GetDenial().GetMarker().ApprovalTtl = durationpb.New(ttl)
			require.NoError(t, protovalidate.Validate(req), "ttl %s", ttl)
		}
	})

	testCases := []struct {
		name     string
		mutate   func(*accessrequestv1.SubmitAccessRequestRequest)
		wantRule string
	}{
		{
			name:     "approver with uppercase",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetDenial().GetMarker().Approver = "DBA-oncall" },
			wantRule: "string.pattern",
		},
		{
			name:     "approver starting with punctuation",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetDenial().GetMarker().Approver = "-dba" },
			wantRule: "string.pattern",
		},
		{
			name: "approver of 65 characters",
			mutate: func(r *accessrequestv1.SubmitAccessRequestRequest) {
				r.GetDenial().GetMarker().Approver = strings.Repeat("a", 65)
			},
			wantRule: "string.pattern",
		},
		{
			name:     "empty approver",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetDenial().GetMarker().Approver = "" },
			wantRule: "string.pattern",
		},
		{
			name: "ttl below one minute",
			mutate: func(r *accessrequestv1.SubmitAccessRequestRequest) {
				r.GetDenial().GetMarker().ApprovalTtl = durationpb.New(59 * time.Second)
			},
			wantRule: "duration.gte_lte",
		},
		{
			name: "ttl above thirty days",
			mutate: func(r *accessrequestv1.SubmitAccessRequestRequest) {
				r.GetDenial().GetMarker().ApprovalTtl = durationpb.New(720*time.Hour + time.Second)
			},
			wantRule: "duration.gte_lte",
		},
		{
			name:     "no ttl",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetDenial().GetMarker().ApprovalTtl = nil },
			wantRule: "required",
		},
		{
			name: "reason of 513 characters",
			mutate: func(r *accessrequestv1.SubmitAccessRequestRequest) {
				r.GetDenial().GetMarker().Reason = strings.Repeat("é", 513)
			},
			wantRule: "string.max_len",
		},
		{
			name: "evaluation id in lowercase",
			mutate: func(r *accessrequestv1.SubmitAccessRequestRequest) {
				r.GetDenial().EvaluationId = strings.ToLower(evaluationID)
			},
			wantRule: "string.pattern",
		},
		{
			name: "evaluation id with a letter outside Crockford base32",
			mutate: func(r *accessrequestv1.SubmitAccessRequestRequest) {
				r.GetDenial().EvaluationId = "01J9Z7QK3M5N8P2R4T6V8X0YIL"
			},
			wantRule: "string.pattern",
		},
		{
			name:     "evaluation id that is a Hub id",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetDenial().EvaluationId = requestID },
			wantRule: "string.pattern",
		},
		{
			name:     "no marker",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetDenial().Marker = nil },
			wantRule: "required",
		},
		{
			name:     "no actions",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetDenial().Actions = nil },
			wantRule: "repeated.min_items",
		},
		{
			name:     "no rule source",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetDenial().RuleSrc = "" },
			wantRule: "string.min_len",
		},
		{
			name:     "principal without roles",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetDenial().GetPrincipal().Roles = nil },
			wantRule: "required",
		},
		{
			name:     "empty idempotency key",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.IdempotencyKey = new("") },
			wantRule: "string.min_len",
		},
		{
			name:     "actor chain four levels deep",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.Actor = actorChain(4) },
			wantRule: "actor.max_depth",
		},
		{
			name:     "actor without an id",
			mutate:   func(r *accessrequestv1.SubmitAccessRequestRequest) { r.GetActor().GetAct()[0].Id = "" },
			wantRule: "string.min_len",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := submitRequest()
			tc.mutate(req)
			requireViolation(t, req, tc.wantRule)
		})
	}
}

func TestAccessRequestValidation(t *testing.T) {
	for _, status := range []accessrequestv1.Status{
		accessrequestv1.Status_STATUS_PENDING,
		accessrequestv1.Status_STATUS_APPROVED,
		accessrequestv1.Status_STATUS_DENIED,
		accessrequestv1.Status_STATUS_EXPIRED,
		accessrequestv1.Status_STATUS_CANCELLED,
	} {
		t.Run(status.String(), func(t *testing.T) {
			require.NoError(t, protovalidate.Validate(accessRequest(status)))
		})
	}

	testCases := []struct {
		name     string
		status   accessrequestv1.Status
		mutate   func(*accessrequestv1.AccessRequest)
		wantRule string
	}{
		{
			name:   "pending with an approval",
			status: accessrequestv1.Status_STATUS_PENDING,
			mutate: func(ar *accessrequestv1.AccessRequest) {
				ar.Approval = accessRequest(accessrequestv1.Status_STATUS_APPROVED).GetApproval()
			},
			wantRule: "access_request.approval_iff_approved",
		},
		{
			name:     "approved without an approval",
			status:   accessrequestv1.Status_STATUS_APPROVED,
			mutate:   func(ar *accessrequestv1.AccessRequest) { ar.Approval = nil },
			wantRule: "access_request.approval_iff_approved",
		},
		{
			name:   "approval that ends when it starts",
			status: accessrequestv1.Status_STATUS_APPROVED,
			mutate: func(ar *accessrequestv1.AccessRequest) {
				ar.GetApproval().ApprovedUntil = ar.GetApproval().GetApprovedAt()
			},
			wantRule: "approval.until_after_at",
		},
		{
			name:   "expiry before creation",
			status: accessrequestv1.Status_STATUS_PENDING,
			mutate: func(ar *accessrequestv1.AccessRequest) {
				ar.ExpiresAt = timestamppb.New(ar.GetCreatedAt().AsTime().Add(-time.Second))
			},
			wantRule: "access_request.expires_after_created",
		},
		{
			name:   "unspecified verification",
			status: accessrequestv1.Status_STATUS_PENDING,
			mutate: func(ar *accessrequestv1.AccessRequest) {
				ar.Verification = accessrequestv1.Verification_VERIFICATION_UNSPECIFIED
			},
			wantRule: "enum.not_in",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			ar := accessRequest(tc.status)
			tc.mutate(ar)
			requireViolation(t, ar, tc.wantRule)
		})
	}
}

// The marker's approval_ttl is a Go duration string in policy outputs and a
// Duration here. Parsing the documented string form yields a value the rules
// accept, and protojson renders it in seconds, as the proto comment says.
func TestMarkerTTLFromPolicyOutput(t *testing.T) {
	ttl, err := time.ParseDuration("4h")
	require.NoError(t, err)

	m := marker()
	m.ApprovalTtl = durationpb.New(ttl)
	require.NoError(t, protovalidate.Validate(m))

	body, err := protojson.Marshal(m)
	require.NoError(t, err)

	var decoded map[string]any
	require.NoError(t, json.Unmarshal(body, &decoded))
	require.Equal(t, "14400s", decoded["approvalTtl"])
}

func requireViolation(t *testing.T, msg proto.Message, wantRule string) {
	t.Helper()

	err := protovalidate.Validate(msg)
	require.Error(t, err)

	var validationErr *protovalidate.ValidationError
	require.ErrorAs(t, err, &validationErr)

	ids := make([]string, 0, len(validationErr.Violations))
	for _, v := range validationErr.Violations {
		ids = append(ids, v.Proto.GetRuleId())
	}
	require.Contains(t, ids, wantRule, "violations: %v", validationErr)
}
