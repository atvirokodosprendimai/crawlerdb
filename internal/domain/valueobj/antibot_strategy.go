package valueobj

// AntiBotStrategy configures the detection and response behavior for anti-bot measures.
type AntiBotStrategy struct {
	Mode       AntiBotMode `json:"mode"`
	MaxRetries int         `json:"max_retries"`
	ProxyPool  []string    `json:"proxy_pool,omitempty"`
	UserAgents []string    `json:"user_agents,omitempty"`
}

// AntiBotEvent records a detected anti-bot measure.
type AntiBotEvent struct {
	EventType string `json:"event_type"` // captcha, challenge, block, rate_limit
	Provider  string `json:"provider"`   // recaptcha, hcaptcha, cloudflare, etc.
	Strategy  string `json:"strategy"`   // skip, retry, solve
	Resolved  bool   `json:"resolved"`
	Details   string `json:"details"`
}
