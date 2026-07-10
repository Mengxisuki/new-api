package controller

import (
	"strings"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

type RegistrationInvitationRequiredError struct{}

func (e *RegistrationInvitationRequiredError) Error() string {
	return "registration requires an invitation code"
}

type RegistrationInvitationInvalidError struct{}

func (e *RegistrationInvitationInvalidError) Error() string {
	return "invitation code is invalid"
}

func resolveRegistrationInviterID(affCode string) (int, error) {
	normalizedAffCode := strings.TrimSpace(affCode)
	if normalizedAffCode == "" {
		if common.RegisterRequireInvitationEnabled {
			return 0, &RegistrationInvitationRequiredError{}
		}
		return 0, nil
	}

	inviterID, err := model.GetUserIdByAffCode(normalizedAffCode)
	if err != nil || inviterID == 0 {
		return 0, &RegistrationInvitationInvalidError{}
	}

	return inviterID, nil
}
