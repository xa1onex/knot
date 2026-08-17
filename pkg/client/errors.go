package client

import (
	"errors"
	"fmt"

	"github.com/knot-infra/knot/pkg/apierrors"
)

// APIError is the typed Node API error surface for all client shells.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("%s: %s", e.Code, e.Message)
	}
	return e.Message
}

func AsAPIError(err error) (*APIError, bool) {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae, true
	}
	return nil, false
}

func IsCode(err error, code string) bool {
	ae, ok := AsAPIError(err)
	return ok && ae.Code == code
}

func IsUnauthorized(err error) bool {
	return IsCode(err, apierrors.CodeUnauthorized) ||
		IsCode(err, apierrors.CodeInvalidCredentials) ||
		IsCode(err, apierrors.CodeTokenExpired) ||
		IsCode(err, apierrors.CodeTokenRevoked)
}

func IsForbidden(err error) bool   { return IsCode(err, apierrors.CodeForbidden) }
func IsNotFound(err error) bool    { return IsCode(err, apierrors.CodeNotFound) }
func IsValidation(err error) bool  { return IsCode(err, apierrors.CodeValidation) }
func IsConflict(err error) bool    { return IsCode(err, apierrors.CodeConflict) }
func IsQuotaExceeded(err error) bool {
	return IsCode(err, apierrors.CodeQuotaExceeded)
}
