package application

import (
	"context"
	"fmt"

	"git.grassecon.net/grassrootseconomics/common/phone"
	storedb "git.grassecon.net/grassrootseconomics/sarafu-vise/store/db"
	"github.com/grassrootseconomics/go-vise/resource"
	"gopkg.in/leonelquinteros/gotext.v1"
)

// InviteValidRecipient sends an invitation to the valid phone number.
func (h *MenuHandlers) InviteValidRecipient(ctx context.Context, sym string, input []byte) (resource.Result, error) {
	var res resource.Result
	store := h.userdataStore
	smsservice := h.smsService

	sessionId, ok := ctx.Value("SessionId").(string)
	if !ok {
		return res, fmt.Errorf("missing session")
	}

	code := codeFromCtx(ctx)
	l := gotext.NewLocale(translationDir, code)
	l.AddDomain("default")

	recipient, err := store.ReadEntry(ctx, sessionId, storedb.DATA_TEMPORARY_VALUE)
	if err != nil {
		logg.ErrorCtxf(ctx, "Failed to read invalid recipient info", "error", err)
		return res, err
	}

	if !phone.IsValidPhoneNumber(string(recipient)) {
		logg.InfoCtxf(ctx, "corrupted recipient", "key", storedb.DATA_TEMPORARY_VALUE, "recipient", recipient)
		return res, nil
	}

	_, err = smsservice.Accountservice.SendUpsellSMS(ctx, sessionId, string(recipient))
	if err != nil {
		res.Content = l.Get("Your invite request for %s to Sarafu Network failed. Please try again later.", string(recipient))
		return res, nil
	}
	res.Content = l.Get("Your invitation to %s to join Sarafu Network has been sent.", string(recipient))
	return res, nil
}
