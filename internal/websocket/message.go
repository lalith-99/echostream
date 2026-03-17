package websocket

// InboundMessage is sent from the client over WebSocket.
type InboundMessage struct {
	Type      string `json:"type"` // subscribe, unsubscribe, typing
	ChannelID string `json:"channel_id,omitempty"`
	Body      string `json:"body,omitempty"`
}

// OutboundEvent is sent from the server to the client over WebSocket.
type OutboundEvent struct {
	Type      string `json:"type"` // message, typing, subscribed, unsubscribed, error
	ChannelID string `json:"channel_id,omitempty"`
	Message   any    `json:"message,omitempty"`
	UserID    string `json:"user_id,omitempty"`
	Error     string `json:"error,omitempty"`
}
