package agent

// Agent follows docs/agent_definition.md. Credentials are not part of this contract.
type AgentAvatar struct {
	Emoji *string `json:"emoji"`
	URL   *string `json:"url"`
}

type AgentRole struct {
	Type         string   `json:"type"`
	Title        string   `json:"title"`
	Specialties  []string `json:"specialties"`
	Capabilities []string `json:"capabilities"`
}

type AgentModelParameters struct {
	Temperature float64  `json:"temperature"`
	MaxTokens   int64    `json:"max_tokens"`
	TopP        *float64 `json:"top_p"`
}

type AgentModelFallback struct {
	Enabled bool             `json:"enabled"`
	Models  []ModelReference `json:"models"`
}

type AgentModel struct {
	Provider   string               `json:"provider"`
	Name       string               `json:"name"`
	Parameters AgentModelParameters `json:"parameters"`
	Fallback   AgentModelFallback   `json:"fallback"`
}

type AgentPrompts struct {
	Identity                 string `json:"identity"`
	TaskInstruction          string `json:"task_instruction"`
	ReasoningInstruction     string `json:"reasoning_instruction"`
	CollaborationInstruction string `json:"collaboration_instruction"`
	CommunicationInstruction string `json:"communication_instruction"`
	OutputInstruction        string `json:"output_instruction"`
}

type AgentTools struct {
	Enabled             bool     `json:"enabled"`
	Allowed             []string `json:"allowed"`
	Denied              []string `json:"denied"`
	RequireConfirmation []string `json:"require_confirmation"`
}

type AgentContext struct {
	Instructions     string   `json:"instructions"`
	Sources          []string `json:"sources"`
	MaxContextTokens *int64   `json:"max_context_tokens"`
}

type AgentCollaboration struct {
	Enabled              bool  `json:"enabled"`
	CanDelegate          bool  `json:"can_delegate"`
	CanReceiveTasks      bool  `json:"can_receive_tasks"`
	CanMessageAgents     bool  `json:"can_message_agents"`
	CanMentionAgents     bool  `json:"can_mention_agents"`
	CanCreateThreads     bool  `json:"can_create_threads"`
	CanJoinThreads       bool  `json:"can_join_threads"`
	CanReviewOtherAgents bool  `json:"can_review_other_agents"`
	CanRequestReview     bool  `json:"can_request_review"`
	MaxDelegationDepth   int64 `json:"max_delegation_depth"`
}

type AgentCommunication struct {
	DefaultMessageType  string   `json:"default_message_type"`
	AllowedMessageTypes []string `json:"allowed_message_types"`
	MentionPolicy       string   `json:"mention_policy"`
	ReplyPolicy         string   `json:"reply_policy"`
}

type AgentOutputInclude struct {
	Confidence       bool `json:"confidence"`
	References       bool `json:"references"`
	ReasoningSummary bool `json:"reasoning_summary"`
}

type AgentOutput struct {
	Format   string                 `json:"format"`
	Language string                 `json:"language"`
	Schema   map[string]interface{} `json:"schema"`
	Include  AgentOutputInclude     `json:"include"`
}

type AgentBehavior struct {
	Autonomy              string `json:"autonomy"`
	AskWhenUncertain      bool   `json:"ask_when_uncertain"`
	AskWhenMissingContext bool   `json:"ask_when_missing_context"`
	StopWhenBlocked       bool   `json:"stop_when_blocked"`
	RetryOnFailure        bool   `json:"retry_on_failure"`
	MaxRetries            int64  `json:"max_retries"`
}

type AgentPermissionsFiles struct {
	Read  bool `json:"read"`
	Write bool `json:"write"`
}

type AgentPermissionsNetwork struct {
	Access bool `json:"access"`
}

type AgentPermissionsExternalActions struct {
	Allowed bool `json:"allowed"`
}

type AgentPermissionsAgents struct {
	Message  bool `json:"message"`
	Delegate bool `json:"delegate"`
}

type AgentPermissions struct {
	Files           AgentPermissionsFiles           `json:"files"`
	Network         AgentPermissionsNetwork         `json:"network"`
	ExternalActions AgentPermissionsExternalActions `json:"external_actions"`
	Agents          AgentPermissionsAgents          `json:"agents"`
}

type AgentMessengerProfile struct {
	DisplayName *string `json:"display_name"`
	AvatarURL   *string `json:"avatar_url"`
}

type AgentMessenger struct {
	Provider string                `json:"provider"`
	UserID   *string               `json:"user_id"`
	Username *string               `json:"username"`
	Bot      bool                  `json:"bot"`
	Profile  AgentMessengerProfile `json:"profile"`
}

type AgentLifecycle struct {
	Enabled   bool    `json:"enabled"`
	Version   int64   `json:"version"`
	CreatedAt *string `json:"created_at"`
	UpdatedAt *string `json:"updated_at"`
}

type AgentRuntime struct {
	Status string  `json:"status"`
	Error  *string `json:"error"`
}

type Agent struct {
	Runtime       AgentRuntime           `json:"runtime"`
	ID            string                 `json:"id"`
	Name          string                 `json:"name"`
	DisplayName   string                 `json:"display_name"`
	Description   string                 `json:"description"`
	Avatar        AgentAvatar            `json:"avatar"`
	Tags          []string               `json:"tags"`
	Role          AgentRole              `json:"role"`
	Model         AgentModel             `json:"model"`
	Prompts       AgentPrompts           `json:"prompts"`
	Tools         AgentTools             `json:"tools"`
	Context       AgentContext           `json:"context"`
	Collaboration AgentCollaboration     `json:"collaboration"`
	Communication AgentCommunication     `json:"communication"`
	Output        AgentOutput            `json:"output"`
	Behavior      AgentBehavior          `json:"behavior"`
	Permissions   AgentPermissions       `json:"permissions"`
	Messenger     AgentMessenger         `json:"messenger"`
	Lifecycle     AgentLifecycle         `json:"lifecycle"`
	Metadata      map[string]interface{} `json:"metadata"`
}

type ModelReference struct {
	Provider string `json:"provider"`
	Name     string `json:"name"`
}
