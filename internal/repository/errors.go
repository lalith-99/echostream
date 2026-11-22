package repository

import "errors"

// ErrMembershipUnavailable covers both undiscoverable channels and ineligible invitees.
var ErrMembershipUnavailable = errors.New("channel or workspace user unavailable")

// ErrEmailRegistered is returned when another account already owns this email.
var ErrEmailRegistered = errors.New("email already registered")
