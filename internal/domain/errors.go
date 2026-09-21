package domain

import "fmt"

// ErrorCode 是机器可读的业务错误码。
type ErrorCode string

const (
	CodeValidation              ErrorCode = "VALIDATION_ERROR"
	CodeDigestMismatch          ErrorCode = "DIGEST_MISMATCH"
	CodeUnknownEntity           ErrorCode = "UNKNOWN_ENTITY"
	CodeUnknownRelation         ErrorCode = "UNKNOWN_RELATION"
	CodeUnknownPlan             ErrorCode = "UNKNOWN_PLAN"
	CodeUnknownCommitment       ErrorCode = "UNKNOWN_COMMITMENT"
	CodeReference               ErrorCode = "REFERENCE_ERROR"
	CodeDuplicateEvent          ErrorCode = "DUPLICATE_EVENT"
	CodeSequenceConflict       ErrorCode = "SEQUENCE_CONFLICT"
	CodeDuplicateRelation       ErrorCode = "DUPLICATE_RELATION"
	CodePartyMerged             ErrorCode = "PARTY_MERGED"
	CodeInvalidTransition       ErrorCode = "INVALID_STATE_TRANSITION"
	CodeMissingApproval         ErrorCode = "MISSING_APPROVAL"
	CodeTextVersion             ErrorCode = "TEXT_VERSION_INVALID"
	CodePlanConstraint         ErrorCode = "PLAN_CONSTRAINT_VIOLATED"
	CodeCityConfirmationMissing ErrorCode = "CITY_CONFIRMATION_REQUIRED"
	CodeDuplicateCommitment     ErrorCode = "DUPLICATE_COMMITMENT"
)

// Error 携带稳定错误码与面向调用方的说明，HTTP 层据此映射状态码。
type Error struct {
	Code    ErrorCode `json:"code"`
	Message string    `json:"message"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Code, e.Message)
}

// NewError 构造一个业务错误。
func NewError(code ErrorCode, format string, args ...any) *Error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}
