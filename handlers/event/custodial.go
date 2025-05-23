package event

import (
	"context"
	"fmt"

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
func (eh *EventsUpdater) handleCustodialRegistration(ctx context.Context, ev any) error {
	o, ok := ev.(*apievent.EventCustodialRegistration)
	if !ok {
		return fmt.Errorf("invalid event for custodial registration")
	}
	return eh.HandleCustodialRegistration(ctx, o)
}

func (eu *EventsUpdater) HandleCustodialRegistration(ctx context.Context, ev *apievent.EventCustodialRegistration) error {
	_, userStore, err := eu.getStore(ctx)
	if err != nil {
		return err
	}
	identity, err := store.IdentityFromAddress(ctx, userStore, ev.Account)
	if err != nil {
		return err
	}
	logg.DebugCtxf(ctx, "received custodial registration event", "identity", identity)
	return nil
}
