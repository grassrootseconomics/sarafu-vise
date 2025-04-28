package sms

import (
	"context"
	"fmt"

	"git.defalsify.org/vise.git/logging"
	"git.grassecon.net/grassrootseconomics/common/phone"
	"git.grassecon.net/grassrootseconomics/sarafu-api/remote"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
)

var (
	logg = logging.NewVanilla().WithDomain("smsservice")
)

type SmsService struct {
	Accountservice remote.AccountService
	Userdatastore  store.UserDataStore
}

// SendUpsellSMS will send an invitation SMS to an unregistered phone number
func (smsservice *SmsService) SendUpsellSMS(ctx context.Context, inviterPhone, inviteePhone string) error {
	if !phone.IsValidPhoneNumber(inviterPhone) {
		return fmt.Errorf("invalid inviter phone number %v", inviterPhone)
	}

	if !phone.IsValidPhoneNumber(inviteePhone) {
		return fmt.Errorf("Invalid invitee phone number %v", inviteePhone)
	}
	_, err := smsservice.Accountservice.SendUpsellSMS(ctx, inviterPhone, inviteePhone)
	if err != nil {
		return fmt.Errorf("Failed to send upsell sms: %v", err)
	}
	return nil
}

// sendPINResetSMS will send an SMS to a user's phonenumber in the event that the associated account's PIN has been reset.
func (smsService *SmsService) SendPINResetSMS(ctx context.Context, adminPhoneNumber, blockedPhoneNumber string) error {
	formattedAdminPhone, err := phone.FormatPhoneNumber(adminPhoneNumber)
	if err != nil {
		return fmt.Errorf("failed to format admin phone number: %w", err)
	}

	formattedBlockedPhone, err := phone.FormatPhoneNumber(blockedPhoneNumber)
	if err != nil {
		return fmt.Errorf("failed to format blocked phone number: %w", err)
	}

	if !phone.IsValidPhoneNumber(formattedAdminPhone) {
		return fmt.Errorf("invalid admin phone number")
	}
	if !phone.IsValidPhoneNumber(formattedBlockedPhone) {
		return fmt.Errorf("invalid blocked phone number")
	}

	err = smsService.Accountservice.SendPINResetSMS(ctx, formattedAdminPhone, formattedBlockedPhone)
	if err != nil {
		return fmt.Errorf("failed to send pin reset sms: %v", err)
	}

	return nil
}

// SendAddressSMS will triger an SMS when a user navigates to the my address node.The SMS will be sent to the associated phonenumber.
func (smsService *SmsService) SendAddressSMS(ctx context.Context) error {
	store := smsService.Userdatastore
	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return fmt.Errorf("missing session")
	}

	publicKey, err := store.ReadEntry(ctx, sessionId, storedb.DATA_PUBLIC_KEY)
	if err != nil {
		logg.ErrorCtxf(ctx, "failed to read publicKey entry with", "key", storedb.DATA_PUBLIC_KEY, "error", err)
		return err
	}

	originPhone, err := phone.FormatPhoneNumber(sessionId)
	if err != nil {
		logg.DebugCtxf(ctx, "Failed to format origin phonenumber", "sessionid", sessionId)
		return nil
	}

	if !phone.IsValidPhoneNumber(originPhone) {
		logg.InfoCtxf(ctx, "Invalid origin phone number", "origin phonenumber", originPhone)
		return fmt.Errorf("invalid origin phone number")
	}
	err = smsService.Accountservice.SendAddressSMS(ctx, string(publicKey), originPhone)
	if err != nil {
		logg.DebugCtxf(ctx, "Failed to send address sms", "error", err)
		return fmt.Errorf("Failed to send address sms: %v", err)
	}
	return nil
}
