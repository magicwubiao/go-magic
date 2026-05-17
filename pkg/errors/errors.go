package errors

import (
	"fmt"
)

// ErrorCode represents an error code
type ErrorCode string

// Error codes
const (
	// General errors
	ErrCodeUnknown          ErrorCode = "UNKNOWN"
	ErrCodeInvalidInput     ErrorCode = "INVALID_INPUT"
	ErrCodeInternalError    ErrorCode = "INTERNAL_ERROR"
	ErrCodeNotFound         ErrorCode = "NOT_FOUND"
	ErrCodeTimeout          ErrorCode = "TIMEOUT"
	ErrCodeUnauthorized     ErrorCode = "UNAUTHORIZED"
	ErrCodePermissionDenied ErrorCode = "PERMISSION_DENIED"

	// Agent errors
	ErrCodeAgentInit    ErrorCode = "AGENT_INIT_FAILED"
	ErrCodeAgentProcess ErrorCode = "AGENT_PROCESS_FAILED"
	ErrCodeAgentTimeout ErrorCode = "AGENT_TIMEOUT"

	// Tool errors
	ErrCodeToolNotFound      ErrorCode = "TOOL_NOT_FOUND"
	ErrCodeToolExecution     ErrorCode = "TOOL_EXECUTION_FAILED"
	ErrCodeToolTimeout       ErrorCode = "TOOL_TIMEOUT"
	ErrCodeToolInvalidParams ErrorCode = "TOOL_INVALID_PARAMS"

	// Provider errors
	ErrCodeProviderInit      ErrorCode = "PROVIDER_INIT_FAILED"
	ErrCodeProviderCall      ErrorCode = "PROVIDER_CALL_FAILED"
	ErrCodeProviderAuth      ErrorCode = "PROVIDER_AUTH_FAILED"
	ErrCodeProviderRateLimit ErrorCode = "PROVIDER_RATE_LIMIT"
	ErrCodeProviderNotFound  ErrorCode = "PROVIDER_NOT_FOUND"

	// Gateway errors
	ErrCodeGatewayInit       ErrorCode = "GATEWAY_INIT_FAILED"
	ErrCodeGatewayConnect    ErrorCode = "GATEWAY_CONNECT_FAILED"
	ErrCodeGatewayDisconnect ErrorCode = "GATEWAY_DISCONNECT_FAILED"
	ErrCodeGatewayMessage    ErrorCode = "GATEWAY_MESSAGE_FAILED"

	// Session errors
	ErrCodeSessionNotFound ErrorCode = "SESSION_NOT_FOUND"
	ErrCodeSessionCreate   ErrorCode = "SESSION_CREATE_FAILED"
	ErrCodeSessionExpired  ErrorCode = "SESSION_EXPIRED"

	// Skill errors
	ErrCodeSkillNotFound  ErrorCode = "SKILL_NOT_FOUND"
	ErrCodeSkillLoad      ErrorCode = "SKILL_LOAD_FAILED"
	ErrCodeSkillExecution ErrorCode = "SKILL_EXECUTION_FAILED"

	// Config errors
	ErrCodeConfigNotFound ErrorCode = "CONFIG_NOT_FOUND"
	ErrCodeConfigInvalid  ErrorCode = "CONFIG_INVALID"
)

// AppError represents an application error with code
type AppError struct {
	Code    ErrorCode
	Message string
	Err     error
}

// Error implements the error interface
func (e *AppError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("[%s] %s: %v", e.Code, e.Message, e.Err)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Unwrap returns the wrapped error
func (e *AppError) Unwrap() error {
	return e.Err
}

// NewAppError creates a new AppError
func NewAppError(code ErrorCode, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
	}
}

// Wrap wraps an error with code and message
func Wrap(err error, code ErrorCode, message string) *AppError {
	return &AppError{
		Code:    code,
		Message: message,
		Err:     err,
	}
}

// NewAgentInitError creates an agent initialization error
func NewAgentInitError(err error) *AppError {
	return Wrap(err, ErrCodeAgentInit, "failed to initialize agent")
}

// NewAgentProcessError creates an agent process error
func NewAgentProcessError(err error) *AppError {
	return Wrap(err, ErrCodeAgentProcess, "failed to process message")
}

// NewToolNotFoundError creates a tool not found error
func NewToolNotFoundError(name string) *AppError {
	return NewAppError(ErrCodeToolNotFound, fmt.Sprintf("tool not found: %s", name))
}

// NewToolExecutionError creates a tool execution error
func NewToolExecutionError(err error) *AppError {
	return Wrap(err, ErrCodeToolExecution, "failed to execute tool")
}

// NewToolTimeoutError creates a tool timeout error
func NewToolTimeoutError(name string) *AppError {
	return NewAppError(ErrCodeToolTimeout, fmt.Sprintf("tool execution timeout: %s", name))
}

// NewToolInvalidParamsError creates a tool invalid parameters error
func NewToolInvalidParamsError(name string) *AppError {
	return NewAppError(ErrCodeToolInvalidParams, fmt.Sprintf("invalid parameters for tool: %s", name))
}

// NewProviderNotFoundError creates a provider not found error
func NewProviderNotFoundError(name string) *AppError {
	return NewAppError(ErrCodeProviderNotFound, fmt.Sprintf("provider not found: %s", name))
}

// NewProviderCallError creates a provider call error
func NewProviderCallError(err error) *AppError {
	return Wrap(err, ErrCodeProviderCall, "failed to call provider")
}

// NewProviderAuthError creates a provider auth error
func NewProviderAuthError() *AppError {
	return NewAppError(ErrCodeProviderAuth, "provider authentication failed")
}

// NewProviderRateLimitError creates a provider rate limit error
func NewProviderRateLimitError(provider string) *AppError {
	return NewAppError(ErrCodeProviderRateLimit, fmt.Sprintf("provider rate limit exceeded: %s", provider))
}

// NewSessionNotFoundError creates a session not found error
func NewSessionNotFoundError(id string) *AppError {
	return NewAppError(ErrCodeSessionNotFound, fmt.Sprintf("session not found: %s", id))
}

// NewSessionCreateError creates a session create error
func NewSessionCreateError(err error) *AppError {
	return Wrap(err, ErrCodeSessionCreate, "failed to create session")
}

// NewSessionExpiredError creates a session expired error
func NewSessionExpiredError(id string) *AppError {
	return NewAppError(ErrCodeSessionExpired, fmt.Sprintf("session expired: %s", id))
}

// NewSkillNotFoundError creates a skill not found error
func NewSkillNotFoundError(name string) *AppError {
	return NewAppError(ErrCodeSkillNotFound, fmt.Sprintf("skill not found: %s", name))
}

// NewSkillLoadError creates a skill load error
func NewSkillLoadError(err error, name string) *AppError {
	return Wrap(err, ErrCodeSkillLoad, fmt.Sprintf("failed to load skill: %s", name))
}

// NewSkillExecutionError creates a skill execution error
func NewSkillExecutionError(err error, name string) *AppError {
	return Wrap(err, ErrCodeSkillExecution, fmt.Sprintf("failed to execute skill: %s", name))
}

// NewConfigNotFoundError creates a config not found error
func NewConfigNotFoundError(path string) *AppError {
	return NewAppError(ErrCodeConfigNotFound, fmt.Sprintf("config file not found: %s", path))
}

// NewConfigInvalidError creates a config invalid error
func NewConfigInvalidError(err error, field string) *AppError {
	return Wrap(err, ErrCodeConfigInvalid, fmt.Sprintf("invalid config: %s", field))
}

// NewGatewayInitError creates a gateway init error
func NewGatewayInitError(err error) *AppError {
	return Wrap(err, ErrCodeGatewayInit, "failed to initialize gateway")
}

// NewGatewayConnectError creates a gateway connect error
func NewGatewayConnectError(err error, platform string) *AppError {
	return Wrap(err, ErrCodeGatewayConnect, fmt.Sprintf("failed to connect to %s", platform))
}

// NewGatewayDisconnectError creates a gateway disconnect error
func NewGatewayDisconnectError(err error, platform string) *AppError {
	return Wrap(err, ErrCodeGatewayDisconnect, fmt.Sprintf("failed to disconnect from %s", platform))
}

// NewGatewayMessageError creates a gateway message error
func NewGatewayMessageError(err error) *AppError {
	return Wrap(err, ErrCodeGatewayMessage, "failed to process gateway message")
}

// NewInvalidInputError creates an invalid input error
func NewInvalidInputError(msg string) *AppError {
	return NewAppError(ErrCodeInvalidInput, msg)
}

// NewInternalError creates an internal error
func NewInternalError(err error) *AppError {
	return Wrap(err, ErrCodeInternalError, "internal error")
}

// NewUnauthorizedError creates an unauthorized error
func NewUnauthorizedError() *AppError {
	return NewAppError(ErrCodeUnauthorized, "unauthorized")
}

// NewPermissionDeniedError creates a permission denied error
func NewPermissionDeniedError() *AppError {
	return NewAppError(ErrCodePermissionDenied, "permission denied")
}

// IsNotFound checks if the error is a not found error
func IsNotFound(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodeNotFound
	}
	return false
}

// IsTimeout checks if the error is a timeout error
func IsTimeout(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodeTimeout
	}
	return false
}

// IsAuthError checks if the error is an authentication error
func IsAuthError(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodeUnauthorized || appErr.Code == ErrCodeProviderAuth
	}
	return false
}

// IsRateLimitError checks if the error is a rate limit error
func IsRateLimitError(err error) bool {
	if appErr, ok := err.(*AppError); ok {
		return appErr.Code == ErrCodeProviderRateLimit
	}
	return false
}

// Errorf creates a new AppError with formatted message.
// This is a drop-in replacement for fmt.Errorf that uses AppError format.
// Usage: errors.Errorf(errors.ErrCodeInvalidInput, "invalid value: %s", value)
func Errorf(code ErrorCode, format string, args ...interface{}) *AppError {
	return &AppError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
	}
}

// WrapError wraps an existing error with a formatted message.
// Usage: errors.WrapError(err, errors.ErrCodeInvalidInput, "validation failed: %s", detail)
func WrapError(err error, code ErrorCode, format string, args ...interface{}) *AppError {
	return &AppError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Err:     err,
	}
}

// WithMessage creates an AppError with just a message, using ErrCodeInternalError.
func WithMessage(code ErrorCode, message string) *AppError {
	return NewAppError(code, message)
}
