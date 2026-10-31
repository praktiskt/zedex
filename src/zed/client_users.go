package zed

import (
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"
)

type OrganizationID string

type OrganizationUIPermissions struct {
	CanInstallGithubApp bool `json:"can_install_github_app"`
}

type Organization struct {
	ID            OrganizationID            `json:"id"`
	Name          string                    `json:"name"`
	IsPersonal    bool                      `json:"is_personal"`
	UIPermissions OrganizationUIPermissions `json:"ui_permissions"`
}

type OrganizationEditPredictionConfiguration struct {
	IsEnabled         bool `json:"is_enabled"`
	IsFeedbackEnabled bool `json:"is_feedback_enabled"`
}

type OrganizationConfiguration struct {
	IsZedModelProviderEnabled    bool                                    `json:"is_zed_model_provider_enabled"`
	IsAgentThreadFeedbackEnabled bool                                    `json:"is_agent_thread_feedback_enabled"`
	IsCollaborationEnabled       bool                                    `json:"is_collaboration_enabled"`
	EditPrediction               OrganizationEditPredictionConfiguration `json:"edit_prediction"`
}

type GetAuthenticatedUserResponse struct {
	User                        AuthenticatedUser                            `json:"user"`
	FeatureFlags                []string                                     `json:"feature_flags"`
	Organizations               []Organization                               `json:"organizations"`
	DefaultOrganizationID       *OrganizationID                              `json:"default_organization_id"`
	PlansByOrganization         map[OrganizationID]Plan                      `json:"plans_by_organization"`
	ConfigurationByOrganization map[OrganizationID]OrganizationConfiguration `json:"configuration_by_organization"`
	Plan                        PlanInfo                                     `json:"plan"`
}

type AuthenticatedUser struct {
	IDV2                     string  `json:"id_v2"`
	LegacyUserID             int     `json:"legacy_user_id"`
	MetricsID                string  `json:"metrics_id"`
	Username                 string  `json:"username"`
	AvatarURL                string  `json:"avatar_url"`
	GithubLogin              string  `json:"github_login"`
	Name                     *string `json:"name"`
	IsStaff                  bool    `json:"is_staff"`
	AcceptedTosAt            *string `json:"accepted_tos_at"`
	HasConnectedToCollabOnce bool    `json:"has_connected_to_collab_once"`
}

type Plan string

const (
	ZedFree     Plan = "zed_free"
	ZedPro      Plan = "zed_pro"
	ZedProTrial Plan = "zed_pro_trial"
	ZedBusiness Plan = "zed_business"
	ZedVip      Plan = "zed_vip"
	ZedStudent  Plan = "zed_student"
)

type PlanInfo struct {
	Plan               Plan                `json:"plan_v3"`
	SubscriptionPeriod *SubscriptionPeriod `json:"subscription_period"`
	Usage              CurrentUsage        `json:"usage"`
	TrialStartedAt     *string             `json:"trial_started_at"`
	IsAccountTooYoung  bool                `json:"is_account_too_young"`
	HasOverdueInvoices bool                `json:"has_overdue_invoices"`
}

type SubscriptionPeriod struct {
	StartedAt string `json:"started_at"`
	EndedAt   string `json:"ended_at"`
}

type AcceptTermsOfServiceResponse struct {
	User AuthenticatedUser `json:"user"`
}

type LlmToken string

type CreateLlmTokenResponse struct {
	Token LlmToken `json:"token"`
}

type PlanResponse struct {
	Plan  Plan          `json:"plan"`
	Usage *CurrentUsage `json:"usage"`
}

type CurrentUsage struct {
	EditPredictions UsageData `json:"edit_predictions"`
}

type UsageData struct {
	Used  uint32 `json:"used"`
	Limit any    `json:"limit"`
}

type UsageLimit struct {
	Limited   *int32 `json:"limited"`
	Unlimited bool   `json:"unlimited"`
}

// UserIdentity is the identity zedex reports for a user.
type UserIdentity struct {
	// LegacyUserID is the numeric id Zed knows the user by.
	LegacyUserID int

	// Username is the login the user goes by.
	Username string

	// Name is the display name of the user. It is optional.
	Name string

	// GithubLogin is the GitHub account of the user, if there is one.
	GithubLogin string

	// AvatarURL is the picture of the user.
	AvatarURL string
}

// NewGetAuthenticatedUsersResponse builds the response Zed expects from
// /client/users/me. The session name is shown in Zed's title bar (as the
// organization name) so that users can tell their sessions apart.
func NewGetAuthenticatedUsersResponse() GetAuthenticatedUserResponse {
	return NewGetAuthenticatedUsersResponseFor(UserIdentity{
		LegacyUserID: 1,
		Username:     "zedex",
	})
}

// NewGetAuthenticatedUsersResponseFor builds a session specific response.
func NewGetAuthenticatedUsersResponseFor(identity UserIdentity) GetAuthenticatedUserResponse {
	legacyUserId := identity.LegacyUserID
	sessionName := identity.Username
	now := time.Now().UTC().Format(time.RFC3339Nano)
	orgID := OrganizationID("zedex")

	githubLogin := identity.GithubLogin
	if githubLogin == "" {
		githubLogin = sessionName
	}

	var name *string
	if identity.Name != "" {
		name = proto.String(identity.Name)
	}

	return GetAuthenticatedUserResponse{
		User: AuthenticatedUser{
			IDV2:                     strconv.Itoa(legacyUserId),
			LegacyUserID:             legacyUserId,
			MetricsID:                "",
			Username:                 sessionName,
			AvatarURL:                identity.AvatarURL,
			GithubLogin:              githubLogin,
			Name:                     name,
			IsStaff:                  true,
			AcceptedTosAt:            proto.String(now),
			HasConnectedToCollabOnce: true,
		},
		FeatureFlags: []string{"zed-pro", "notebooks", "debugger", "llm-closed-beta", "thread-auto-capture"},
		Organizations: []Organization{
			{
				ID:            orgID,
				Name:          sessionName,
				IsPersonal:    false,
				UIPermissions: OrganizationUIPermissions{CanInstallGithubApp: true},
			},
		},
		DefaultOrganizationID: &orgID,
		PlansByOrganization: map[OrganizationID]Plan{
			orgID: ZedPro,
		},
		ConfigurationByOrganization: map[OrganizationID]OrganizationConfiguration{
			orgID: {
				IsZedModelProviderEnabled:    true,
				IsAgentThreadFeedbackEnabled: true,
				IsCollaborationEnabled:       true,
				EditPrediction: OrganizationEditPredictionConfiguration{
					IsEnabled:         true,
					IsFeedbackEnabled: true,
				},
			},
		},
		Plan: PlanInfo{
			Plan:               ZedPro,
			SubscriptionPeriod: nil,
			Usage: CurrentUsage{
				EditPredictions: UsageData{
					Used:  0,
					Limit: "unlimited",
				},
			},
			TrialStartedAt:     proto.String(now),
			IsAccountTooYoung:  false,
			HasOverdueInvoices: false,
		},
	}
}

func NewLLMToken() CreateLlmTokenResponse {
	return CreateLlmTokenResponse{
		Token: LlmToken("le-token"),
	}
}
