package apierrors

import (
	"encoding/json"
	"errors"
	"net/http"
)

const (
	CodeUnauthorized       = "unauthorized"
	CodeForbidden          = "forbidden"
	CodeNotFound           = "not_found"
	CodeInvalidCredentials = "invalid_credentials"
	CodeTokenExpired       = "token_expired"
	CodeTokenRevoked       = "token_revoked"
	CodeValidation         = "validation_error"
	CodeInsecureConfig     = "insecure_config"
	CodeConflict           = "conflict"
	CodeQuotaExceeded      = "quota_exceeded"
	CodeInternal           = "internal"
)

type Body struct {
	Error Detail `json:"error"`
}

type Detail struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Error struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *Error) Error() string {
	if e == nil {
		return ""
	}
	return e.Code + ": " + e.Message
}

func New(code, message string, status int) *Error {
	return &Error{Code: code, Message: message, HTTPStatus: status}
}

func Unauthorized(msg string) *Error {
	return New(CodeUnauthorized, msg, http.StatusUnauthorized)
}

func Forbidden(msg string) *Error {
	return New(CodeForbidden, msg, http.StatusForbidden)
}

func NotFound(msg string) *Error {
	return New(CodeNotFound, msg, http.StatusNotFound)
}

func InvalidCredentials(msg string) *Error {
	return New(CodeInvalidCredentials, msg, http.StatusUnauthorized)
}

func TokenExpired(msg string) *Error {
	return New(CodeTokenExpired, msg, http.StatusUnauthorized)
}

func TokenRevoked(msg string) *Error {
	return New(CodeTokenRevoked, msg, http.StatusUnauthorized)
}

func Validation(msg string) *Error {
	return New(CodeValidation, msg, http.StatusBadRequest)
}

func Conflict(msg string) *Error {
	return New(CodeConflict, msg, http.StatusConflict)
}

func Internal(msg string) *Error {
	return New(CodeInternal, msg, http.StatusInternalServerError)
}

func Write(w http.ResponseWriter, err error) {
	var ae *Error
	if errors.As(err, &ae) {
		write(w, ae.HTTPStatus, ae.Code, ae.Message)
		return
	}
	write(w, http.StatusInternalServerError, CodeInternal, "internal error")
}

func WriteCode(w http.ResponseWriter, status int, code, message string) {
	write(w, status, code, message)
}

func write(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(Body{Error: Detail{Code: code, Message: message}})
}

// ParseBody extracts error code from an API error JSON response.
func ParseBody(data []byte) (code, message string) {
	var b Body
	if err := json.Unmarshal(data, &b); err != nil {
		return "", string(data)
	}
	return b.Error.Code, b.Error.Message
}
