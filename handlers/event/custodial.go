package event

import (
	"context"

	"git.defalsify.org/vise.git/persist"
	apievent "git.grassecon.net/grassrootseconomics/sarafu-api/event"
	"git.grassecon.net/grassrootseconomics/sarafu-vise/store"
)

const (
	// TODO: consolidate with loaded flags
	accountCreatedFlag = 9
)

// handle custodial registration.
//
// TODO: implement account created in userstore instead, so that
// the need for persister and state use here is eliminated (it
// introduces concurrency risks)
func (eh *EventsHandler) HandleCustodialRegistration(ctx context.Context, userStore *store.UserDataStore, pr *persist.Persister, ev *apievent.EventCustodialRegistration) error {
	identity, err := store.IdentityFromAddress(ctx, userStore, ev.Account)
	if err != nil {
		return err
	}
	err = pr.Load(identity.SessionId)
	if err != nil {
		return err
	}
	st := pr.GetState()
	st.SetFlag(accountCreatedFlag)
	return pr.Save(identity.SessionId)
}
