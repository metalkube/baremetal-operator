package controllers

import (
	"fmt"
)

// EmptyBMCAddressError is returned when the BMC address field
// for a host is empty
type EmptyBMCAddressError struct {
	message string
}

func (e EmptyBMCAddressError) Error() string {
	return fmt.Sprintf("Empty BMC address %s",
		e.message)
}

// EmptyBMCSecretError is returned when the BMC secret
// for a host is empty
type EmptyBMCSecretError struct {
	message string
}

func (e EmptyBMCSecretError) Error() string {
	return fmt.Sprintf("No BMC CredentialsName defined %s",
		e.message)
}

// ResolveBMCSecretRefError is returned when the BMC secret
// for a host is defined but cannot be found
type ResolveBMCSecretRefError struct {
	message string
}

func (e ResolveBMCSecretRefError) Error() string {
	return fmt.Sprintf("BMC CredentialsName secret doesn't exist %s",
		e.message)
}

// NoDataInSecretError is returned when host configuration
// data were not found in referenced secret
type NoDataInSecretError struct {
	secret string
	key    string
}

func (e NoDataInSecretError) Error() string {
	return fmt.Sprintf("Secret %s does not contain key %s", e.secret, e.key)
}
