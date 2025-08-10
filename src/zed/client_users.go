package zed

import (
	"time"

	"google.golang.org/protobuf/proto"
)

type GetAuthenticatedUserResponse struct {
	User         AuthenticatedUser `json:"user"`
	FeatureFlags []string          `json:"feature_flags"`
	Plan         PlanInfo          `json:"plan"`
}

type AuthenticatedUser struct {
	ID            int     `json:"id"`
	MetricsID     string  `json:"metrics_id"`
	AvatarURL     string  `json:"avatar_url"`
	GithubLogin   string  `json:"github_login"`
	Name          *string `json:"name"`
	IsStaff       bool    `json:"is_staff"`
	AcceptedTosAt *string `json:"accepted_tos_at"`
}

type PlanInfo struct {
	Plan                       string              `json:"plan"`
	SubscriptionPeriod         *SubscriptionPeriod `json:"subscription_period"`
	Usage                      CurrentUsage        `json:"usage"`
	TrialStartedAt             *string             `json:"trial_started_at"`
	IsUsageBasedBillingEnabled bool                `json:"is_usage_based_billing_enabled"`
	IsAccountTooYoung          bool                `json:"is_account_too_young"`
	HasOverdueInvoices         bool                `json:"has_overdue_invoices"`
}

type SubscriptionPeriod struct {
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
}

type AcceptTermsOfServiceResponse struct {
	User AuthenticatedUser `json:"user"`
}

type LlmToken struct {
	Token string `json:"token"`
}

type CreateLlmTokenResponse struct {
	Token LlmToken `json:"token"`
}

type Plan int

const (
	// ZedFree is the free plan
	ZedFree Plan = iota
	// ZedPro is the pro plan
	ZedPro
	// ZedProTrial is the pro trial plan
	ZedProTrial
)

func (p Plan) String() string {
	plans := [...]string{"Free", "ZedPro", "ZedProTrial"}
	return plans[p]
}

type GetSubscriptionResponse struct {
	Plan  Plan          `json:"plan"`
	Usage *CurrentUsage `json:"usage"`
}

type CurrentUsage struct {
	ModelRequests   UsageData `json:"model_requests"`
	EditPredictions UsageData `json:"edit_predictions"`
}

type UsageData struct {
	Used  uint32 `json:"used"`
	Limit string `json:"limit"`
}

type UsageLimit struct {
	Limited   *int32 `json:"limited"`
	Unlimited bool   `json:"unlimited"`
}

func NewGetAuthenticatedUsersResponse() GetAuthenticatedUserResponse {
	return GetAuthenticatedUserResponse{
		User: AuthenticatedUser{
			ID:        1,
			MetricsID: "",
			AvatarURL: "",
			// TODO: Fix me
			GithubLogin:   "",
			Name:          nil,
			IsStaff:       false,
			AcceptedTosAt: proto.String(time.Now().UTC().Format(time.RFC3339)),
		},
		FeatureFlags: []string{},
		Plan: PlanInfo{
			Plan:               "Free",
			SubscriptionPeriod: nil,
			Usage: CurrentUsage{
				ModelRequests: UsageData{
					Used:  0,
					Limit: "unlimited",
				},
				EditPredictions: UsageData{
					Used:  0,
					Limit: "unlimited",
				},
			},
			TrialStartedAt:             proto.String(time.Now().UTC().Format(time.RFC3339)),
			IsUsageBasedBillingEnabled: false,
			IsAccountTooYoung:          false,
			HasOverdueInvoices:         false,
		},
	}
}
